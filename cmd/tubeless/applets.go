package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-gl/glfw/v3.4/glfw"

	"github.com/moozd/tubeless/pkg/applet"
	"github.com/moozd/tubeless/pkg/applet/cef"
	"github.com/moozd/tubeless/pkg/config"
	"github.com/moozd/tubeless/pkg/render"
	"github.com/moozd/tubeless/pkg/screen"
)

// appletFPS caps how often a page paints. CEF's own default is 30.
const appletFPS = 60

// appletCloseWait bounds how long shutdown waits for pages to close.
const appletCloseWait = 2 * time.Second

// appletDialWait bounds the dial back to a `tubeless exec` socket.
const appletDialWait = time.Second

// appletRequestQueue is how many open requests may wait for the render
// loop. More than that in one frame is a runaway, not a user.
const appletRequestQueue = 16

// forwardAppletRequests hands every open request the parser queued to
// the applet host. A full queue drops the request with a log line; the
// `tubeless exec` that sent it times out and says so.
func forwardAppletRequests(work *screen.Screen, out chan<- []byte) {
	if out == nil {
		return
	}
	for _, req := range work.DrainAppletRequests() {
		select {
		case out <- req:
		default:
			log.Printf("applets: request queue full, dropped a request")
		}
	}
}

// openApplet is one page and the `tubeless exec` that owns its pane.
type openApplet struct {
	id         int
	browser    *cef.Browser
	conn       net.Conn
	events     chan applet.Message
	cols, rows int
	w, h       int
	scale      float32
	isClosing  bool
	// isPaneFocused is tmux's view (focus reports from exec); hasFocus
	// is whether the page has the keyboard (see syncFocus).
	isPaneFocused bool
	hasFocus      bool
	isBound       bool
}

// appletHost owns CEF for this window. It opens pages for `tubeless
// exec` requests, pumps CEF's message loop from the render loop, and
// moves page pixels into the renderer.
type appletHost struct {
	cacheDir   string
	background uint32
	requests   <-chan []byte
	applets    map[int]*openApplet
	lastR      *render.Renderer
	input      appletInput
	server     *applet.Server
	theme      applet.Theme
	isStarted  bool
	startErr   error
}

// appletsDir is where applets are installed, one folder per applet.
func appletsDir() string {
	return filepath.Join(xdgDir("XDG_CONFIG_HOME", ".config"), "tubeless", "applets")
}

func xdgDir(env string, fallback ...string) string {
	if dir := os.Getenv(env); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("applets: home dir: %v", err)
	}
	return filepath.Join(append([]string{home}, fallback...)...)
}

// startAppletHost starts CEF. It returns nil when this build has no
// applet support or CEF failed to start; the terminal works either way.
// startAppletHost prepares applet support. CEF itself starts only on
// the first `tubeless exec` (see startCEF): it costs about a second and
// close to a gigabyte across its processes, which a window with no
// applet must not pay. It returns nil when this build has no applet
// support.
func startAppletHost(background [3]float32, requests <-chan []byte) *appletHost {
	if !cef.Available() {
		return nil
	}
	h := &appletHost{background: argb(background), requests: requests}
	h.applets = make(map[int]*openApplet)
	h.server = applet.NewServer()
	h.input.scanOffset = scancodeOffset()
	return h
}

// startCEF starts CEF once, on the main thread. A failure is kept:
// CEF cannot start twice in one process, and every later request gets
// the same reason.
func (h *appletHost) startCEF() error {
	if h.isStarted || h.startErr != nil {
		return h.startErr
	}
	h.startErr = h.initCEF()
	if h.startErr != nil {
		log.Printf("applets disabled: %v", h.startErr)
		return h.startErr
	}
	h.isStarted = true
	h.input.prefix = readTmuxPrefix()
	return nil
}

func (h *appletHost) initCEF() error {
	opts, err := cefOptions()
	if err != nil {
		return err
	}
	cache, err := os.MkdirTemp("", "tubeless-cef-")
	if err != nil {
		return fmt.Errorf("cef cache dir: %w", err)
	}
	opts.CacheDir = cache
	opts.LogFile = filepath.Join(cache, "cef.log")
	opts.Background = h.background
	cef.SetWake(glfw.PostEmptyEvent)
	cef.SetSchemeHandler(h.serveScheme)
	cef.SetConsoleHandler(h.logConsole)
	if err := cef.Init(opts); err != nil {
		os.RemoveAll(cache)
		return err
	}
	h.cacheDir = cache
	log.Printf("applets: cef started from %s", filepath.Dir(opts.LibPath))
	return nil
}

// serveScheme answers tubeless:// requests. CEF calls it on its IO
// thread; the server locks its own state.
func (h *appletHost) serveScheme(req cef.SchemeRequest) cef.SchemeResponse {
	r := h.server.Serve(req.BrowserID, req.URL, req.Method, req.Body)
	return cef.SchemeResponse{Status: r.Status, MIME: r.MIME, Body: r.Body}
}

// logConsole writes a page's console output to the session log, so
// applet authors see their logs and errors.
func (h *appletHost) logConsole(b *cef.Browser, level int, msg, source string, line int) {
	for _, a := range h.applets {
		if a.browser == b {
			log.Printf("applet %d console: %s (%s:%d)", a.id, msg, source, line)
			return
		}
	}
}

// argb packs a linear-RGB theme color as the opaque sRGB ARGB value CEF
// takes for a page background.
func argb(linear [3]float32) uint32 {
	c := uint32(0xff) << 24
	for i, v := range linear {
		c |= uint32(config.LinearToSRGB8(v)) << (16 - 8*i)
	}
	return c
}

// Frame takes new requests, pumps CEF, follows pane changes, uploads
// new pixels and places every page on its visible cells. It reports
// whether the scene must be redrawn. Safe on nil.
func (h *appletHost) Frame(r *render.Renderer, scr *screen.Screen, cs *cellSize, cfg config.Config) bool {
	if h == nil {
		return false
	}
	h.updateTheme(appletTheme(cfg, cs))
	h.takeRequests(cs)
	h.pump()
	h.takeExits()
	isNewRenderer := r != h.lastR
	h.lastR = r
	dirty := false
	for _, a := range h.applets {
		h.bind(a)
		a.handleEvents()
		a.resize(cs)
		dirty = a.upload(r, isNewRenderer) || dirty
	}
	h.syncFocus()
	dirty = h.dropClosed(r) || dirty
	r.SetAppletSpans(appletSpans(scr, h.sizes()))
	return dirty
}

// takeRequests opens a page for every queued `tubeless exec` request.
func (h *appletHost) takeRequests(cs *cellSize) {
	for {
		select {
		case payload := <-h.requests:
			h.open(payload, cs)
		default:
			return
		}
	}
}

func (h *appletHost) open(payload []byte, cs *cellSize) {
	req, err := applet.DecodeRequest(payload)
	if err != nil {
		log.Printf("applets: %v", err)
		return
	}
	if !isExecSocket(req.Socket) {
		log.Printf("applets: refused request with socket %q (not a local tubeless exec)", req.Socket)
		return
	}
	conn, err := net.DialTimeout("unix", req.Socket, appletDialWait)
	if err != nil {
		log.Printf("applets: dial %s: %v", req.Socket, err)
		return
	}
	if err := h.startCEF(); err != nil {
		sendApplet(conn, applet.Message{Type: applet.MsgError, Error: "applets are off in this window: " + err.Error()})
		conn.Close()
		return
	}
	a, err := h.start(req, conn, cs)
	if err != nil {
		log.Printf("applets: open %q: %v", req.Name, err)
		sendApplet(conn, applet.Message{Type: applet.MsgError, Error: err.Error()})
		conn.Close()
		return
	}
	h.applets[a.id] = a
	sendApplet(conn, applet.Message{Type: applet.MsgReady})
}

func (h *appletHost) start(req applet.Request, conn net.Conn, cs *cellSize) (*openApplet, error) {
	if req.ID < 0 || req.ID >= screen.AppletMaxID {
		return nil, fmt.Errorf("applet id %d out of range", req.ID)
	}
	if _, isUsed := h.applets[req.ID]; isUsed {
		return nil, fmt.Errorf("applet id %d is in use, run it again", req.ID)
	}
	inst, err := appletInstance(req)
	if err != nil {
		return nil, err
	}
	a := &openApplet{id: req.ID, conn: conn, cols: req.Cols, rows: req.Rows, scale: cs.dpiX}
	a.w, a.h = a.pixelSize(cs)
	h.server.Add(inst)
	a.browser, err = cef.NewBrowser(applet.PageURL(req.ID), a.w, a.h, a.scale, h.background, appletFPS)
	if err != nil {
		h.server.Remove(req.ID)
		return nil, err
	}
	a.events = make(chan applet.Message, 16)
	go readAppletMessages(conn, a.events)
	return a, nil
}

// isExecSocket reports whether path is a socket `tubeless exec` on this
// machine listens on. Any program's output (a file shown with cat,
// output over ssh) can carry an open request, so the window must never
// dial an arbitrary socket it names.
func isExecSocket(path string) bool {
	dir, name := filepath.Split(path)
	if filepath.Clean(dir) != execSocketDir() {
		return false
	}
	pid, ok := strings.CutSuffix(strings.TrimPrefix(name, "exec-"), ".sock")
	if !ok || !strings.HasPrefix(name, "exec-") || pid == "" {
		return false
	}
	_, err := strconv.Atoi(pid)
	return err == nil
}

// appletInstance finds an installed applet and reads its manifest.
func appletInstance(req applet.Request) (applet.Instance, error) {
	if req.Name == "" || req.Name != filepath.Base(req.Name) {
		return applet.Instance{}, fmt.Errorf("invalid applet name %q", req.Name)
	}
	dir := filepath.Join(appletsDir(), req.Name)
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return applet.Instance{}, fmt.Errorf("no applet %q (looked for %s)", req.Name, dir)
		}
		return applet.Instance{}, fmt.Errorf("applet %q: %w", req.Name, err)
	}
	m, err := applet.LoadManifest(dir)
	if err != nil {
		return applet.Instance{}, err
	}
	return applet.Instance{ID: req.ID, Name: req.Name, Dir: dir, Args: req.Args, Cwd: req.Cwd, Manifest: m}, nil
}

// bind lets a page call its applet's API once CEF has created it.
func (h *appletHost) bind(a *openApplet) {
	if a.isBound || a.browser.ID() == 0 {
		return
	}
	h.server.BindBrowser(a.id, a.browser.ID())
	a.isBound = true
}

// takeExits ends every applet that called tubeless.exit().
func (h *appletHost) takeExits() {
	for {
		select {
		case e := <-h.server.Exits():
			h.exit(e)
		default:
			return
		}
	}
}

func (h *appletHost) exit(e applet.Exit) {
	a, ok := h.applets[e.ID]
	if !ok {
		return
	}
	sendApplet(a.conn, applet.Message{Type: applet.MsgExit, Code: e.Code, Output: e.Output})
	a.close()
}

// updateTheme hands a changed terminal theme to new and open pages.
func (h *appletHost) updateTheme(t applet.Theme) {
	if t == h.theme {
		return
	}
	h.theme = t
	h.server.SetTheme(t)
	script, err := applet.ThemeScript(t)
	if err != nil {
		log.Printf("applets: %v", err)
		return
	}
	for _, a := range h.applets {
		a.browser.ExecJS(script)
	}
}

func readAppletMessages(conn net.Conn, out chan<- applet.Message) {
	if err := applet.ReadMessages(conn, out); err != nil {
		log.Printf("applets: %v", err)
	}
}

func sendApplet(conn net.Conn, m applet.Message) {
	if err := applet.WriteMessage(conn, m); err != nil {
		log.Printf("applets: %v", err)
	}
}

// pump runs CEF's message loop once per frame while pages are open,
// and otherwise only when CEF asked for work.
func (h *appletHost) pump() {
	if !h.isStarted {
		return
	}
	_, isDue := cef.NextPump(time.Now())
	if !isDue && len(h.applets) == 0 {
		return
	}
	cef.Pump()
}

// handleEvents applies what `tubeless exec` reported since last frame.
func (a *openApplet) handleEvents() {
	for {
		select {
		case m, ok := <-a.events:
			if !ok {
				a.close()
				return
			}
			a.handleEvent(m)
		default:
			return
		}
	}
}

func (a *openApplet) handleEvent(m applet.Message) {
	switch m.Type {
	case applet.MsgResize:
		a.cols, a.rows = m.Cols, m.Rows
	case applet.MsgFocus:
		a.isPaneFocused = m.Focused
	case applet.MsgClose:
		a.close()
	}
}

func (a *openApplet) close() {
	if a.isClosing {
		return
	}
	a.isClosing = true
	a.browser.Close()
}

func (a *openApplet) pixelSize(cs *cellSize) (int, int) {
	return int(float32(a.cols) * cs.w), int(float32(a.rows) * cs.h)
}

func (a *openApplet) resize(cs *cellSize) {
	w, h := a.pixelSize(cs)
	if a.w == w && a.h == h && a.scale == cs.dpiX {
		return
	}
	a.w, a.h, a.scale = w, h, cs.dpiX
	a.browser.Resize(w, h, a.scale)
}

func (a *openApplet) upload(r *render.Renderer, all bool) bool {
	return a.browser.TakeFrame(all, func(f *cef.Frame) {
		tex := r.AppletTexture(a.id)
		tex.Resize(f.W, f.H)
		for _, d := range f.Dirty {
			tex.UpdateRect(d.X, d.Y, d.W, d.H, f.Pix, f.W)
		}
	})
}

// dropClosed frees every page CEF finished closing.
func (h *appletHost) dropClosed(r *render.Renderer) bool {
	dropped := false
	for id, a := range h.applets {
		if !a.browser.Closed() {
			continue
		}
		a.browser.Release()
		a.conn.Close()
		r.DropApplet(id)
		h.server.Remove(id)
		delete(h.applets, id)
		h.input.forget(a)
		dropped = true
	}
	return dropped
}

func (h *appletHost) sizes() map[int]appletGrid {
	sizes := make(map[int]appletGrid, len(h.applets))
	for id, a := range h.applets {
		sizes[id] = appletGrid{cols: a.cols, rows: a.rows}
	}
	return sizes
}

// Shutdown closes every page, waits for CEF to confirm, and stops CEF.
// Safe on nil.
func (h *appletHost) Shutdown() {
	if h == nil || !h.isStarted {
		return
	}
	for _, a := range h.applets {
		a.close()
	}
	deadline := time.Now().Add(appletCloseWait)
	for !h.allClosed() && time.Now().Before(deadline) {
		cef.Pump()
		time.Sleep(10 * time.Millisecond)
	}
	for _, a := range h.applets {
		a.browser.Release()
		a.conn.Close()
	}
	cef.Shutdown()
	if err := os.RemoveAll(h.cacheDir); err != nil {
		log.Printf("applets: remove cef cache: %v", err)
	}
}

func (h *appletHost) allClosed() bool {
	for _, a := range h.applets {
		if !a.browser.Closed() {
			return false
		}
	}
	return true
}
