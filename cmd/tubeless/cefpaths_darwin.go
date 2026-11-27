package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/moozd/tubeless/pkg/applet/cef"
)

const (
	cefFramework = "Chromium Embedded Framework.framework"
	cefLibrary   = "Chromium Embedded Framework"
	cefHelper    = "tubeless Helper"
)

// cefRuntimeDir is the Frameworks folder of the app bundle tubeless
// runs from: `make install-darwin APPLETS=1` puts the CEF framework
// and the helper apps there. TUBELESS_CEF_DIR overrides it with the
// Frameworks folder of another bundle.
func cefRuntimeDir() (string, error) {
	if dir := os.Getenv("TUBELESS_CEF_DIR"); dir != "" {
		return dir, nil
	}
	bundle, err := appBundle()
	if err != nil {
		return "", err
	}
	return filepath.Join(bundle, "Contents", "Frameworks"), nil
}

// appBundle finds the .app around the running binary. The tubeless in
// ~/.local/bin is a symlink into the bundle, so it resolves links
// first.
func appBundle() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find tubeless binary: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolve tubeless binary: %w", err)
	}
	return bundleOf(exe)
}

// bundleOf returns the .app that holds exe in its Contents/MacOS.
func bundleOf(exe string) (string, error) {
	macOS := filepath.Dir(exe)
	bundle := filepath.Dir(filepath.Dir(macOS))
	if filepath.Base(macOS) != "MacOS" || filepath.Ext(bundle) != ".app" {
		return "", fmt.Errorf("%s is not in an app bundle (install with: make install-darwin APPLETS=1)", exe)
	}
	return bundle, nil
}

// cefOptions locates the CEF framework and the helper in the bundle.
// CEF loads its resources and locales from the framework itself.
func cefOptions() (cef.Options, error) {
	dir, err := cefRuntimeDir()
	if err != nil {
		return cef.Options{}, err
	}
	framework := filepath.Join(dir, cefFramework)
	resources := filepath.Join(framework, "Resources")
	return cef.Options{
		LibPath:        filepath.Join(framework, cefLibrary),
		SubprocessPath: filepath.Join(dir, cefHelper+".app", "Contents", "MacOS", cefHelper),
		ResourcesDir:   resources,
		LocalesDir:     resources,
		FrameworkDir:   framework,
		MainBundle:     filepath.Dir(filepath.Dir(dir)),
	}, nil
}
