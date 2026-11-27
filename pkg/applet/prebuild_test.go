package applet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrebuildRunsWithoutSourcesOrBuild(t *testing.T) {
	src := t.TempDir()
	files := map[string]string{
		"main.ts":                  "import './style.css'; const n: number = 2; console.log(n)",
		"style.css":                "body { color: red }",
		"node_modules/w/worker.js": "w",
		"node_modules/w/big.js":    "not shipped",
		"applet.toml":              "static = [\"node_modules/w/worker.js\"]\n",
	}
	for name, body := range files {
		p := filepath.Join(src, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	dst := filepath.Join(t.TempDir(), "t")
	if err := Prebuild(src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "node_modules/w/big.js")); err == nil {
		t.Fatal("a file not in static was shipped")
	}
	os.RemoveAll(src)

	s := NewServer()
	m, err := LoadManifest(dst)
	if err != nil {
		t.Fatal(err)
	}
	s.Add(Instance{ID: 1, Name: "t", Dir: dst, Cwd: dst, Manifest: m})
	s.BindBrowser(1, 1)
	page := s.Serve(1, PageURL(1), "GET", nil)
	if !strings.Contains(string(page.Body), `href="/main.css"`) {
		t.Fatalf("shell has no bundled css: %q", page.Body)
	}
	if js := s.Serve(1, PageURL(1)+"main.js", "GET", nil); js.Status != 200 || strings.Contains(string(js.Body), ": number") {
		t.Fatalf("main.js = %d %q", js.Status, js.Body)
	}
	if w := s.Serve(1, PageURL(1)+"node_modules/w/worker.js", "GET", nil); string(w.Body) != "w" {
		t.Fatalf("static file = %d %q", w.Status, w.Body)
	}
}

func TestPrebuildRejectsStaticOutsideTheApplet(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "main.js"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(src, "applet.toml"), []byte("static = [\"../x\"]\n"), 0o644)
	if err := Prebuild(src, filepath.Join(t.TempDir(), "t")); err == nil {
		t.Fatal("static ../x was accepted")
	}
}
