package applet

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// prebuiltMarker marks an applet folder that Prebuild wrote: its files
// are served as they are, with no esbuild run and no node_modules.
const prebuiltMarker = ".prebuilt"

func isPrebuilt(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, prebuiltMarker))
	return err == nil
}

// prebuiltFile reads urlPath from a prebuilt applet. A missing file is
// not an error: the caller then tries other sources.
func prebuiltFile(dir, urlPath string) ([]byte, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path.Clean("/"+urlPath))))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("prebuilt applet: %w", err)
	}
	return data, true, nil
}

// Prebuild turns the applet in src (with its npm packages installed)
// into a folder dst that runs with no build step and no node_modules:
// the bundle, applet.toml, an optional index.html, and the files its
// manifest lists as static. Release packages ship applets this way.
func Prebuild(src, dst string) error {
	m, err := LoadManifest(src)
	if err != nil {
		return err
	}
	bd, err := buildApplet(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("prebuild %s: %w", dst, err)
	}
	for urlPath, data := range bd.files {
		if err := writeFileAll(filepath.Join(dst, filepath.FromSlash(urlPath)), data); err != nil {
			return err
		}
	}
	for _, name := range append([]string{"applet.toml", "index.html"}, m.Static...) {
		if err := copyIntoPrebuilt(src, dst, name); err != nil {
			return err
		}
	}
	return writeFileAll(filepath.Join(dst, prebuiltMarker), nil)
}

// copyIntoPrebuilt copies the file or folder name from src to the same
// place in dst. A missing applet.toml or index.html is fine; a missing
// static entry is an error in the manifest.
func copyIntoPrebuilt(src, dst, name string) error {
	rel := filepath.Clean(name)
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, "../") {
		return fmt.Errorf("static %q leaves the applet folder", name)
	}
	from := filepath.Join(src, rel)
	st, err := os.Stat(from)
	if errors.Is(err, os.ErrNotExist) && (name == "applet.toml" || name == "index.html") {
		return nil
	}
	if err != nil {
		return fmt.Errorf("static %q: %w", name, err)
	}
	if !st.IsDir() {
		return copyFile(from, filepath.Join(dst, rel))
	}
	return filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		sub, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		return copyFile(p, filepath.Join(dst, sub))
	})
}

func copyFile(from, to string) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return fmt.Errorf("copy %s: %w", from, err)
	}
	return writeFileAll(to, data)
}

func writeFileAll(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
