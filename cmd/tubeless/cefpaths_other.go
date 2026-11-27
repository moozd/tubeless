//go:build !darwin

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/moozd/tubeless/pkg/applet"
	"github.com/moozd/tubeless/pkg/applet/cef"
)

// cefRuntimeDir is where the first `tubeless exec` downloads libcef and
// its resources (or where `make cef-runtime` links them).
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
		SubprocessPath: cefHelperPath(dir),
		ResourcesDir:   dir,
		LocalesDir:     filepath.Join(dir, "locales"),
	}, nil
}

// cefHelperPath finds tubeless-cef-helper: next to the tubeless binary
// (where an install puts it), else in the runtime dir (where `make
// cef-runtime` puts it). The downloaded runtime has no helper.
func cefHelperPath(runtimeDir string) string {
	const name = "tubeless-cef-helper"
	exe, err := os.Executable()
	if err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		beside := filepath.Join(filepath.Dir(exe), name)
		if _, err := os.Stat(beside); err == nil {
			return beside
		}
	}
	return filepath.Join(runtimeDir, name)
}

// isCEFRuntimeReady reports whether the runtime is downloaded.
func isCEFRuntimeReady() bool {
	return applet.RuntimeReady(cefRuntimeDir())
}

// ensureCEFRuntime downloads the runtime on the first `tubeless exec`
// of a machine, before the window needs it.
func ensureCEFRuntime(progress io.Writer) error {
	dir := cefRuntimeDir()
	if applet.RuntimeReady(dir) {
		return nil
	}
	fmt.Fprintf(progress, "tubeless: downloading the applet runtime (CEF %s), only once\n",
		strings.SplitN(applet.CEFVersion, "+", 2)[0])
	return applet.InstallRuntime(dir, progress)
}
