package applet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestApplet installs an applet with the given files in a temp dir
// and serves it as id 7 from browser 3.
func newTestApplet(t *testing.T, files map[string]string, args []string) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer()
	s.Add(Instance{ID: 7, Name: "t", Dir: dir, Args: args, Cwd: dir, Manifest: m})
	s.BindBrowser(7, 3)
	return s, dir
}

func TestServeShellRuntimeAndBundle(t *testing.T) {
	s, _ := newTestApplet(t, map[string]string{"main.ts": "const n: number = 1; console.log(n)"}, nil)
	page := s.Serve(3, PageURL(7), "GET", nil)
	if page.Status != 200 || !strings.Contains(string(page.Body), `src="/main.js"`) {
		t.Fatalf("shell = %d %q", page.Status, page.Body)
	}
	js := s.Serve(3, PageURL(7)+"main.js", "GET", nil)
	if js.Status != 200 || strings.Contains(string(js.Body), ": number") {
		t.Fatalf("main.js not transpiled: %d %q", js.Status, js.Body)
	}
	rt := s.Serve(3, PageURL(7)+"tubeless.js", "GET", nil)
	if !strings.Contains(string(rt.Body), `"name":"t"`) {
		t.Fatalf("runtime has no init: %q", rt.Body)
	}
}

func TestServeReadRespectsArgs(t *testing.T) {
	s, _ := newTestApplet(t, map[string]string{"main.js": "", "a.txt": "A", "b.txt": "B"}, []string{"a.txt"})
	if r := s.Serve(3, PageURL(7)+"api/read?path=a.txt", "GET", nil); r.Status != 200 || string(r.Body) != "A" {
		t.Fatalf("read arg = %d %q", r.Status, r.Body)
	}
	if r := s.Serve(3, PageURL(7)+"api/read?path=b.txt", "GET", nil); r.Status != 403 {
		t.Fatalf("read of a file not in args = %d", r.Status)
	}
	if r := s.Serve(9, PageURL(7)+"api/read?path=a.txt", "GET", nil); r.Status != 403 {
		t.Fatalf("read from another page = %d", r.Status)
	}
}

func TestServeWriteNeedsPermission(t *testing.T) {
	s, dir := newTestApplet(t, map[string]string{"main.js": ""}, []string{"out.txt"})
	if r := s.Serve(3, PageURL(7)+"api/write?path=out.txt", "POST", []byte("x")); r.Status != 403 {
		t.Fatalf("write with default permissions = %d", r.Status)
	}
	s2, dir2 := newTestApplet(t, map[string]string{"main.js": "", "applet.toml": "[permissions]\nwrite = \"args\"\n"}, []string{"out.txt"})
	if r := s2.Serve(3, PageURL(7)+"api/write?path=out.txt", "POST", []byte("x")); r.Status != 200 {
		t.Fatalf("write with write = args: %d %q", r.Status, r.Body)
	}
	if data, _ := os.ReadFile(filepath.Join(dir2, "out.txt")); string(data) != "x" {
		t.Fatalf("written = %q", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "out.txt")); err == nil {
		t.Fatal("refused write still wrote")
	}
}

func TestServeExitQueuesResult(t *testing.T) {
	s, _ := newTestApplet(t, map[string]string{"main.js": ""}, nil)
	s.Serve(3, PageURL(7)+"api/exit", "POST", []byte(`{"code":2,"output":"bye"}`))
	select {
	case e := <-s.Exits():
		if e.ID != 7 || e.Code != 2 || e.Output != "bye" {
			t.Fatalf("exit = %+v", e)
		}
	default:
		t.Fatal("no exit queued")
	}
}

func TestServeStaticStaysInFolder(t *testing.T) {
	s, _ := newTestApplet(t, map[string]string{"main.js": "", "worker.js": "w"}, nil)
	if r := s.Serve(3, PageURL(7)+"worker.js", "GET", nil); string(r.Body) != "w" {
		t.Fatalf("static = %d %q", r.Status, r.Body)
	}
	if r := s.Serve(3, PageURL(7)+"../../etc/passwd", "GET", nil); r.Status != 404 {
		t.Fatalf("path escape = %d", r.Status)
	}
}

func TestServeShowsBuildErrors(t *testing.T) {
	s, _ := newTestApplet(t, map[string]string{"main.ts": "const x: number = ;"}, nil)
	r := s.Serve(3, PageURL(7), "GET", nil)
	if !strings.Contains(string(r.Body), "build failed") {
		t.Fatalf("error page = %q", r.Body)
	}
}

func TestLoadManifestRejectsUnknownAccess(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "applet.toml"), []byte("[permissions]\nread = \"some\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(dir); err == nil {
		t.Fatal("read = some was accepted")
	}
}

func TestServeReadDirsAllowsFilesNextToArgs(t *testing.T) {
	s, dir := newTestApplet(t, map[string]string{
		"main.js": "", "applet.toml": "[permissions]\nread = \"dirs\"\n",
	}, nil)
	docs := filepath.Join(dir, "docs")
	if err := os.MkdirAll(filepath.Join(docs, "img"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "img", "a.png"), []byte("P"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.Add(Instance{ID: 7, Name: "t", Dir: dir, Args: []string{"docs/note.md"}, Cwd: dir, Manifest: Manifest{Permissions: Permissions{Read: AccessDirs}}})
	s.BindBrowser(7, 3)
	if r := s.Serve(3, PageURL(7)+"api/read?path=docs/img/a.png", "GET", nil); r.Status != 200 {
		t.Fatalf("read next to the arg = %d %q", r.Status, r.Body)
	}
	if r := s.Serve(3, PageURL(7)+"api/read?path=main.js", "GET", nil); r.Status != 403 {
		t.Fatalf("read above the arg folder = %d", r.Status)
	}
	if r := s.Serve(3, PageURL(7)+"api/read?path=docs/../../x", "GET", nil); r.Status != 403 {
		t.Fatalf("read that climbs out = %d", r.Status)
	}
}
