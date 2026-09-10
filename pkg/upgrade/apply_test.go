package upgrade

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceFile(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "bin")
	if err := os.WriteFile(dest, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceFile([]byte("new"), dest, 0o755); err != nil {
		t.Fatalf("replaceFile: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "new" {
		t.Fatalf("replaceFile: content = %q, %v, want %q", got, err, "new")
	}
	if _, err := os.Stat(dest + ".new"); !os.IsNotExist(err) {
		t.Fatalf("replaceFile left a stray %s.new behind", dest)
	}
}

func TestStripBundlePrefix(t *testing.T) {
	cases := map[string]string{
		"Tubeless.app/Contents/MacOS/tubeless": "Contents/MacOS/tubeless",
		"Tubeless.app/":                        "",
		"noslash":                              "",
	}
	for in, want := range cases {
		if got := stripBundlePrefix(in); got != want {
			t.Errorf("stripBundlePrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBundleRootFor(t *testing.T) {
	got := bundleRootFor("/Users/mo/Applications/Tubeless.app/Contents/MacOS/tubeless")
	want := "/Users/mo/Applications/Tubeless.app"
	if got != want {
		t.Fatalf("bundleRootFor = %q, want %q", got, want)
	}
}

func buildTarGz(t *testing.T, files map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	path := filepath.Join(t.TempDir(), "asset.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestApplyLinux(t *testing.T) {
	archive := buildTarGz(t, map[string]string{
		"bin/tubeless":        "new-tubeless",
		"bin/tubeless-config": "new-tubeless-config",
	})
	installDir := t.TempDir()
	for _, name := range []string{"tubeless", "tubeless-config"} {
		if err := os.WriteFile(filepath.Join(installDir, name), []byte("old"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	exec, err := applyLinux(archive, installDir)
	if err != nil {
		t.Fatalf("applyLinux: %v", err)
	}
	if want := filepath.Join(installDir, "tubeless"); exec != want {
		t.Errorf("applyLinux exec = %q, want %q", exec, want)
	}
	assertFileContent(t, filepath.Join(installDir, "tubeless"), "new-tubeless")
	assertFileContent(t, filepath.Join(installDir, "tubeless-config"), "new-tubeless-config")
}

func buildAppZip(t *testing.T, files map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	zw.Close()
	path := filepath.Join(t.TempDir(), "asset.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestApplyDarwin(t *testing.T) {
	archive := buildAppZip(t, map[string]string{
		"Tubeless.app/Contents/MacOS/tubeless":          "new-tubeless",
		"Tubeless.app/Contents/MacOS/tubeless-config":   "new-tubeless-config",
		"Tubeless.app/Contents/Resources/tubeless.icns": "new-icns",
		"Tubeless.app/Contents/Info.plist":              "new-plist",
	})
	bundleRoot := filepath.Join(t.TempDir(), "Tubeless.app")
	if err := os.MkdirAll(filepath.Join(bundleRoot, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, "Contents", "MacOS", "tubeless"), []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	exec, err := applyDarwin(archive, bundleRoot)
	if err != nil {
		t.Fatalf("applyDarwin: %v", err)
	}
	if want := filepath.Join(bundleRoot, "Contents", "MacOS", "tubeless"); exec != want {
		t.Errorf("applyDarwin exec = %q, want %q", exec, want)
	}
	assertFileContent(t, filepath.Join(bundleRoot, "Contents", "MacOS", "tubeless"), "new-tubeless")
	assertFileContent(t, filepath.Join(bundleRoot, "Contents", "MacOS", "tubeless-config"), "new-tubeless-config")
	assertFileContent(t, filepath.Join(bundleRoot, "Contents", "Resources", "tubeless.icns"), "new-icns")
	assertFileContent(t, filepath.Join(bundleRoot, "Contents", "Info.plist"), "new-plist")
}

// TestApplyDarwinDropsStaleFiles guards the actual point of swapping the
// whole bundle directory in with rename() rather than patching files
// in-place one by one: a file that existed in the old install but isn't
// in the new release's zip must not survive the upgrade. The old
// per-file-patch approach left the mix of old and new files half-and-half
// under the same code signature — this both fixes that (see applyDarwin's
// doc comment) and, as a side effect, cleans up files an old version left
// behind that the new one no longer ships.
func TestApplyDarwinDropsStaleFiles(t *testing.T) {
	archive := buildAppZip(t, map[string]string{
		"Tubeless.app/Contents/MacOS/tubeless": "new-tubeless",
	})
	bundleRoot := filepath.Join(t.TempDir(), "Tubeless.app")
	stalePath := filepath.Join(bundleRoot, "Contents", "Resources", "old-leftover.txt")
	if err := os.MkdirAll(filepath.Dir(stalePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stalePath, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(bundleRoot, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, "Contents", "MacOS", "tubeless"), []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := applyDarwin(archive, bundleRoot); err != nil {
		t.Fatalf("applyDarwin: %v", err)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("stale file %s survived the swap (err=%v)", stalePath, err)
	}
}

// TestApplyDarwinNoLeftoverStagingOrBackup confirms a successful upgrade
// leaves neither the ".old" backup directory nor the temporary staging
// directory it extracted into behind — applyDarwin cleans up both once
// the swap itself has succeeded.
func TestApplyDarwinNoLeftoverStagingOrBackup(t *testing.T) {
	archive := buildAppZip(t, map[string]string{
		"Tubeless.app/Contents/MacOS/tubeless": "new-tubeless",
	})
	parent := t.TempDir()
	bundleRoot := filepath.Join(parent, "Tubeless.app")
	if err := os.MkdirAll(filepath.Join(bundleRoot, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, "Contents", "MacOS", "tubeless"), []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := applyDarwin(archive, bundleRoot); err != nil {
		t.Fatalf("applyDarwin: %v", err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "Tubeless.app" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("leftover entries in %s after upgrade: %v", parent, names)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Errorf("%s content = %q, want %q", path, got, want)
	}
}
