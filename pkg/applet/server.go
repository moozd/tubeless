package applet

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// hostPrefix starts the URL host of every applet page: an applet with
// id 42 lives at tubeless://applet-42/.
const hostPrefix = "applet-"

// PageURL is where the page of applet id loads from.
func PageURL(id int) string {
	return "tubeless://" + hostPrefix + strconv.Itoa(id) + "/"
}

// Instance is one running applet as the bridge sees it.
type Instance struct {
	ID       int
	Name     string
	Dir      string
	Args     []string
	Cwd      string
	Manifest Manifest
	// BrowserID is the CEF page that may call this instance's API.
	BrowserID int
}

// Exit is an applet ending itself through tubeless.exit().
type Exit struct {
	ID     int
	Code   int
	Output string
}

// Response is the answer to one tubeless:// request.
type Response struct {
	Status int
	MIME   string
	Body   []byte
}

// Server answers every tubeless:// request: the applet pages, their
// bundled code and files, and the bridge API. It is safe for
// concurrent use; CEF calls it on its IO thread.
type Server struct {
	mu        sync.Mutex
	instances map[int]*Instance
	theme     Theme
	bundles   *bundler
	exits     chan Exit
}

// NewServer returns a server with no applets.
func NewServer() *Server {
	return &Server{
		instances: make(map[int]*Instance),
		bundles:   newBundler(),
		exits:     make(chan Exit, 16),
	}
}

// Exits delivers tubeless.exit() calls.
func (s *Server) Exits() <-chan Exit { return s.exits }

// Add starts serving inst.
func (s *Server) Add(inst Instance) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instances[inst.ID] = &inst
}

// Remove stops serving applet id.
func (s *Server) Remove(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.instances, id)
}

// BindBrowser lets the CEF page browserID call applet id's API.
func (s *Server) BindBrowser(id, browserID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if inst, ok := s.instances[id]; ok {
		inst.BrowserID = browserID
	}
}

// SetTheme changes the theme new pages start with.
func (s *Server) SetTheme(t Theme) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.theme = t.withCSS()
}

// ThemeScript is the JS that applies t to an open page.
func ThemeScript(t Theme) (string, error) {
	data, err := json.Marshal(t.withCSS())
	if err != nil {
		return "", fmt.Errorf("encode theme: %w", err)
	}
	return "window.tubeless && window.tubeless._setTheme(" + string(data) + ")", nil
}

func (s *Server) instance(id int) (Instance, Theme, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.instances[id]
	if !ok {
		return Instance{}, s.theme, false
	}
	return *inst, s.theme, true
}

// Serve answers one request from page browserID.
func (s *Server) Serve(browserID int, rawURL, method string, body []byte) Response {
	u, err := url.Parse(rawURL)
	if err != nil {
		return textResponse(400, "bad url")
	}
	id, err := strconv.Atoi(strings.TrimPrefix(u.Host, hostPrefix))
	if err != nil || !strings.HasPrefix(u.Host, hostPrefix) {
		return textResponse(404, "no such applet")
	}
	inst, theme, ok := s.instance(id)
	if !ok {
		return textResponse(404, "applet is closed")
	}
	if strings.HasPrefix(u.Path, "/api/") {
		if inst.BrowserID != browserID {
			return textResponse(403, "api call from another page")
		}
		return s.serveAPI(inst, u, method, body)
	}
	return s.servePage(inst, theme, u.Path)
}

func (s *Server) servePage(inst Instance, theme Theme, urlPath string) Response {
	switch urlPath {
	case "/", "/index.html":
		return s.serveIndex(inst)
	case "/tubeless.js":
		return runtimeResponse(inst, theme)
	case "/tubeless.css":
		return Response{Status: 200, MIME: "text/css", Body: []byte(theme.cssVars() + baseCSS)}
	}
	data, ok, err := s.bundles.file(inst.Dir, urlPath)
	if err != nil {
		return textResponse(500, err.Error())
	}
	if ok {
		return Response{Status: 200, MIME: mimeOf(urlPath), Body: data}
	}
	return serveStatic(inst.Dir, urlPath)
}

// serveIndex returns the applet's own index.html, or the built-in shell
// page around its main.ts.
func (s *Server) serveIndex(inst Instance) Response {
	custom, err := os.ReadFile(filepath.Join(inst.Dir, "index.html"))
	if err == nil {
		return Response{Status: 200, MIME: "text/html", Body: custom}
	}
	if !errors.Is(err, os.ErrNotExist) {
		return textResponse(500, err.Error())
	}
	_, hasCSS, err := s.bundles.file(inst.Dir, "/main.css")
	if err != nil {
		return errorPage(inst.Name, err)
	}
	return Response{Status: 200, MIME: "text/html", Body: []byte(shellPage(inst, hasCSS))}
}

// serveStatic serves a file of the applet folder, such as a worker
// script or an asset. Paths never leave the folder.
func serveStatic(dir, urlPath string) Response {
	clean := path.Clean("/" + urlPath)
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(clean)))
	if err != nil {
		return textResponse(404, "not found: "+clean)
	}
	return Response{Status: 200, MIME: mimeOf(clean), Body: data}
}

func mimeOf(name string) string {
	switch path.Ext(name) {
	case ".js", ".mjs":
		return "text/javascript"
	case ".css":
		return "text/css"
	case ".html":
		return "text/html"
	case ".wasm":
		return "application/wasm"
	}
	if t := mime.TypeByExtension(path.Ext(name)); t != "" {
		return t
	}
	return "application/octet-stream"
}

func textResponse(status int, text string) Response {
	return Response{Status: status, MIME: "text/plain", Body: []byte(text)}
}

func (s *Server) serveAPI(inst Instance, u *url.URL, method string, body []byte) Response {
	switch u.Path {
	case "/api/read":
		return readFileAPI(inst, u.Query().Get("path"))
	case "/api/write":
		if method != "POST" {
			return textResponse(405, "write needs POST")
		}
		return writeFileAPI(inst, u.Query().Get("path"), body)
	case "/api/exit":
		return s.exitAPI(inst, body)
	}
	return textResponse(404, "no api "+u.Path)
}

func readFileAPI(inst Instance, name string) Response {
	full, err := allowedPath(inst, inst.Manifest.Permissions.Read, name)
	if err != nil {
		return textResponse(403, err.Error())
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return textResponse(404, err.Error())
	}
	return Response{Status: 200, MIME: "application/octet-stream", Body: data}
}

func writeFileAPI(inst Instance, name string, body []byte) Response {
	full, err := allowedPath(inst, inst.Manifest.Permissions.Write, name)
	if err != nil {
		return textResponse(403, err.Error())
	}
	if err := os.WriteFile(full, body, 0o644); err != nil {
		return textResponse(500, err.Error())
	}
	return textResponse(200, "ok")
}

// allowedPath resolves name against the applet's cwd and checks it
// against access.
func allowedPath(inst Instance, access Access, name string) (string, error) {
	if name == "" {
		return "", errors.New("no path")
	}
	full := resolvePath(inst.Cwd, name)
	switch access {
	case AccessAll:
		return full, nil
	case AccessArgs:
		for _, arg := range inst.Args {
			if resolvePath(inst.Cwd, arg) == full {
				return full, nil
			}
		}
		return "", fmt.Errorf("%s is not in the applet's args (applet.toml permissions)", name)
	}
	return "", fmt.Errorf("applet %s has no permission for %s", inst.Name, name)
}

func resolvePath(cwd, name string) string {
	if filepath.IsAbs(name) {
		return filepath.Clean(name)
	}
	return filepath.Join(cwd, name)
}

func (s *Server) exitAPI(inst Instance, body []byte) Response {
	var req struct {
		Code   int    `json:"code"`
		Output string `json:"output"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return textResponse(400, "exit wants {code, output}")
	}
	select {
	case s.exits <- Exit{ID: inst.ID, Code: req.Code, Output: req.Output}:
	default:
		log.Printf("applets: exit queue full, dropped exit of %d", inst.ID)
	}
	return textResponse(200, "ok")
}
