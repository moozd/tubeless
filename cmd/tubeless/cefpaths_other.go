//go:build !darwin

package main

import (
	"os"
	"path/filepath"

	"github.com/moozd/tubeless/pkg/applet/cef"
)

// cefRuntimeDir is where `make cef-runtime` puts libcef, its resources
// and tubeless-cef-helper.
func cefRuntimeDir() string {
	if dir := os.Getenv("TUBELESS_CEF_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(xdgDir("XDG_DATA_HOME", ".local", "share"), "tubeless", "cef")
}

// cefOptions locates libcef, its resources and the helper.
func cefOptions() (cef.Options, error) {
	dir := cefRuntimeDir()
	return cef.Options{
		LibPath:        filepath.Join(dir, "libcef.so"),
		SubprocessPath: filepath.Join(dir, "tubeless-cef-helper"),
		ResourcesDir:   dir,
		LocalesDir:     filepath.Join(dir, "locales"),
	}, nil
}
