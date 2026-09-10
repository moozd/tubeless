// Package upgrade implements `tubeless upgrade`: check GitHub for a
// newer release, download this platform/arch's asset, replace the
// installed files in place, and relaunch.
package upgrade

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

// Run checks for a release newer than currentVersion and, if one exists,
// downloads it, replaces the installed binaries/bundle, and relaunches.
// Progress is written to out (typically os.Stdout) since this runs from
// a terminal, not the GUI.
func Run(currentVersion string, out io.Writer) error {
	fmt.Fprintln(out, "Checking for updates...")
	rel, err := latestRelease()
	if err != nil {
		return err
	}
	if rel.Tag == currentVersion {
		fmt.Fprintf(out, "Already up to date (%s).\n", currentVersion)
		return nil
	}

	asset, err := selectAsset(rel)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Downloading %s (%s -> %s)...\n", asset.Name, currentVersion, rel.Tag)
	archivePath, err := download(asset.URL)
	if err != nil {
		return err
	}
	defer os.Remove(archivePath)

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate running executable: %w", err)
	}
	fmt.Fprintln(out, "Installing...")
	newExec, err := apply(archivePath, exePath)
	if err != nil {
		return err
	}

	fmt.Fprintln(out, "Restarting...")
	return restart(newExec)
}

// apply installs archivePath over the current install, dispatching on OS
// since a Linux install is a flat bin/ directory and a macOS one is an
// app bundle (see applyLinux/applyDarwin).
func apply(archivePath, exePath string) (string, error) {
	if runtime.GOOS == "darwin" {
		return applyDarwin(archivePath, bundleRootFor(exePath))
	}
	return applyLinux(archivePath, filepath.Dir(exePath))
}

// download saves url's body to a temp file and returns its path.
func download(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: unexpected status %s", url, resp.Status)
	}

	f, err := os.CreateTemp("", "tubeless-upgrade-*")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", fmt.Errorf("save download: %w", err)
	}
	return f.Name(), nil
}
