package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeApplet makes an applet folder, with an applet.toml when toml is
// not empty.
func makeApplet(t *testing.T, root, name, toml string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if toml == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "applet.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestListApplets(t *testing.T) {
	user, builtIn := t.TempDir(), t.TempDir()
	makeApplet(t, user, "hello", "")
	makeApplet(t, user, "pdf", `title = "My PDF"`)
	makeApplet(t, builtIn, "pdf", `title = "PDF"`)
	makeApplet(t, builtIn, "demo", `title = "Demo"`)
	makeApplet(t, builtIn, "broken", `title = `)
	var warn bytes.Buffer
	missing := filepath.Join(t.TempDir(), "none")
	entries, err := listApplets([]string{user, missing, builtIn}, &warn)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := printApplets(&out, entries); err != nil {
		t.Fatal(err)
	}
	want := `NAME    TITLE   SOURCE
broken  broken  built-in
demo    Demo    built-in
hello   hello   user
pdf     My PDF  user (replaces built-in)
`
	if out.String() != want {
		t.Errorf("list:\n%s\nwant:\n%s", out.String(), want)
	}
	if !strings.Contains(warn.String(), "applet broken") {
		t.Errorf("no warning for a bad applet.toml: %q", warn.String())
	}
}
