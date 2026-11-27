package applet

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimePathKeepsOnlyReleaseAndResources(t *testing.T) {
	cases := map[string]string{
		"cef_binary_x/Release/libcef.so":           "libcef.so",
		"cef_binary_x/Resources/locales/en-US.pak": "locales/en-US.pak",
		"cef_binary_x/include/cef_app.h":           "",
		"cef_binary_x/Release/../../etc/passwd":    "",
		"cef_binary_x/Release":                     "",
	}
	for name, want := range cases {
		got, ok := runtimePath(name)
		if (want == "") == ok || got != want {
			t.Errorf("runtimePath(%q) = %q, %v; want %q", name, got, ok, want)
		}
	}
}

func TestUntarFlattensTheRuntime(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	files := map[string]string{
		"cef/Release/libcef.so":           "lib",
		"cef/Resources/locales/en-US.pak": "pak",
		"cef/README.txt":                  "skip",
	}
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(body))
	}
	tw.Close()
	dir := t.TempDir()
	if err := untar(dir, &buf); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"libcef.so": "lib", "locales/en-US.pak": "pak"} {
		if got, err := os.ReadFile(filepath.Join(dir, path)); err != nil || string(got) != want {
			t.Errorf("%s = %q, %v", path, got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "README.txt")); err == nil {
		t.Error("README.txt was unpacked")
	}
}

func TestRuntimeReadyNeedsTheVersion(t *testing.T) {
	dir := t.TempDir()
	if RuntimeReady(dir) {
		t.Fatal("empty dir is ready")
	}
	os.WriteFile(filepath.Join(dir, versionFile), []byte("1.0\n"), 0o644)
	if RuntimeReady(dir) {
		t.Fatal("old version is ready")
	}
	os.WriteFile(filepath.Join(dir, versionFile), []byte(CEFVersion+"\n"), 0o644)
	if !RuntimeReady(dir) {
		t.Fatal("pinned version is not ready")
	}
}

func TestReplaceDirKeepsTheHelper(t *testing.T) {
	root := t.TempDir()
	dir, tmp := filepath.Join(root, "cef"), filepath.Join(root, "new")
	os.MkdirAll(dir, 0o755)
	os.MkdirAll(tmp, 0o755)
	os.WriteFile(filepath.Join(dir, "tubeless-cef-helper"), []byte("h"), 0o755)
	os.WriteFile(filepath.Join(dir, "old.so"), []byte("o"), 0o644)
	if err := replaceDir(dir, tmp); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "tubeless-cef-helper")); err != nil {
		t.Error("helper was lost")
	}
	if _, err := os.Stat(filepath.Join(dir, "old.so")); err == nil {
		t.Error("old runtime file was kept")
	}
}
