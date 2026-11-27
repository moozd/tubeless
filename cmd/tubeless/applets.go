package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/go-gl/glfw/v3.4/glfw"

	"github.com/moozd/tubeless/pkg/applet/cef"
	"github.com/moozd/tubeless/pkg/config"
	"github.com/moozd/tubeless/pkg/render"
	"github.com/moozd/tubeless/pkg/screen"
)

// appletFPS caps how often a page paints. CEF's own default is 30.
const appletFPS = 60

// appletCloseWait bounds how long shutdown waits for pages to close.
const appletCloseWait = 2 * time.Second

// applet is one open page and where it shows.
type applet struct {
	id      int
	browser *cef.Browser
	w, h    int
	scale   float32
}

// appletHost owns CEF for this window: it pumps CEF's message loop
// from the render loop and moves page pixels into the renderer.
type appletHost struct {
	cacheDir string
	applets  []*applet
	nextID   int
	lastR    *render.Renderer
}

// cefRuntimeDir is where `make cef-runtime` puts libcef, its resources
// and tubeless-cef-helper.
func cefRuntimeDir() string {
	if dir := os.Getenv("TUBELESS_CEF_DIR"); dir != "" {
		return dir
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, _ := os.UserHomeDir()
		data = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(data, "tubeless", "cef")
}

// startAppletHost starts CEF. It returns nil when this build has no
// applet support or CEF failed to start; the terminal works either way.
func startAppletHost(background [3]float32) *appletHost {
	if !cef.Available() {
		return nil
	}
	h, err := newAppletHost(background)
	if err != nil {
		log.Printf("applets disabled: %v", err)
		return nil
	}
	return h
}

func newAppletHost(background [3]float32) (*appletHost, error) {
	dir := cefRuntimeDir()
	cache, err := os.MkdirTemp("", "tubeless-cef-")
	if err != nil {
		return nil, fmt.Errorf("cef cache dir: %w", err)
	}
	opts := cef.Options{
		SubprocessPath: filepath.Join(dir, "tubeless-cef-helper"),
		ResourcesDir:   dir,
		LocalesDir:     filepath.Join(dir, "locales"),
		CacheDir:       cache,
		LogFile:        filepath.Join(cache, "cef.log"),
		Background:     argb(background),
	}
	cef.SetWake(glfw.PostEmptyEvent)
	if err := cef.Init(opts); err != nil {
		os.RemoveAll(cache)
		return nil, err
	}
	log.Printf("applets: cef started from %s", dir)
	return &appletHost{cacheDir: cache}, nil
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

// open starts a page that fills the full grid.
func (h *appletHost) open(url string, w, ht int, scale float32, background uint32) error {
	b, err := cef.NewBrowser(url, w, ht, scale, background, appletFPS)
	if err != nil {
		return err
	}
	h.nextID++
	h.applets = append(h.applets, &applet{id: h.nextID, browser: b, w: w, h: ht, scale: scale})
	return nil
}

// Frame pumps CEF, keeps every page sized to the grid, and uploads new
// pixels. It reports whether the scene must be redrawn. Safe on nil.
func (h *appletHost) Frame(r *render.Renderer, scr *screen.Screen, cs *cellSize) bool {
	if h == nil {
		return false
	}
	h.pump()
	isNewRenderer := r != h.lastR
	h.lastR = r
	w, ht := gridPixels(scr, cs)
	dirty := false
	var spans []render.AppletSpan
	for _, a := range h.applets {
		h.resize(a, w, ht, cs.dpiX)
		dirty = uploadApplet(r, a, isNewRenderer) || dirty
		spans = append(spans, fullGridSpans(a.id, scr.Cols, scr.Rows)...)
	}
	r.SetAppletSpans(spans)
	return dirty
}

// pump runs CEF's message loop once per frame while pages are open,
// and otherwise only when CEF asked for work.
func (h *appletHost) pump() {
	_, isDue := cef.NextPump(time.Now())
	if !isDue && len(h.applets) == 0 {
		return
	}
	cef.Pump()
}

func (h *appletHost) resize(a *applet, w, ht int, scale float32) {
	if a.w == w && a.h == ht && a.scale == scale {
		return
	}
	a.w, a.h, a.scale = w, ht, scale
	a.browser.Resize(w, ht, scale)
}

func uploadApplet(r *render.Renderer, a *applet, all bool) bool {
	return a.browser.TakeFrame(all, func(f *cef.Frame) {
		tex := r.AppletTexture(a.id)
		tex.Resize(f.W, f.H)
		for _, d := range f.Dirty {
			tex.UpdateRect(d.X, d.Y, d.W, d.H, f.Pix, f.W)
		}
	})
}

// gridPixels is the grid's size in framebuffer pixels.
func gridPixels(scr *screen.Screen, cs *cellSize) (int, int) {
	return int(float32(scr.Cols) * cs.w), int(float32(scr.Rows) * cs.h)
}

// fullGridSpans covers every row of the grid with one applet.
func fullGridSpans(id, cols, rows int) []render.AppletSpan {
	spans := make([]render.AppletSpan, 0, rows)
	for y := 0; y < rows; y++ {
		spans = append(spans, render.AppletSpan{
			ID: id, Col: 0, Row: y, Cells: cols,
			U0: 0, V0: float32(y) / float32(rows),
			U1: 1, V1: float32(y+1) / float32(rows),
		})
	}
	return spans
}

// Shutdown closes every page, waits for CEF to confirm, and stops CEF.
// Safe on nil.
func (h *appletHost) Shutdown() {
	if h == nil {
		return
	}
	for _, a := range h.applets {
		a.browser.Close()
	}
	deadline := time.Now().Add(appletCloseWait)
	for !h.allClosed() && time.Now().Before(deadline) {
		cef.Pump()
		time.Sleep(10 * time.Millisecond)
	}
	for _, a := range h.applets {
		a.browser.Release()
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

// openSpikeApplet opens $TUBELESS_APPLET_SPIKE over the full grid: a
// development hook to see a page render before `tubeless exec` exists.
func openSpikeApplet(h *appletHost, shared *atomic.Pointer[screen.Screen], cs *cellSize, background [3]float32) {
	url := os.Getenv("TUBELESS_APPLET_SPIKE")
	if h == nil || url == "" {
		return
	}
	w, ht := gridPixels(shared.Load(), cs)
	if err := h.open(url, w, ht, cs.dpiX, argb(background)); err != nil {
		log.Printf("applets: spike: %v", err)
	}
}
