package upgrade

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// replaceFile atomically overwrites destPath's contents. Writing to a
// sibling temp file first and renaming over the target — rather than
// truncating destPath directly — means a binary that's currently running
// (destPath itself, mid-upgrade) never has its executable file truncated
// out from under it: rename() swaps the directory entry, leaving whatever
// inode the running process already has open untouched.
func replaceFile(data []byte, destPath string, perm os.FileMode) error {
	tmp := destPath + ".new"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, destPath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replace %s: %w", destPath, err)
	}
	return nil
}

// applyLinux extracts archivePath (the linux-<arch>.tar.gz asset, laid
// out as bin/tubeless + bin/tubeless-config — see the Makefile's
// package-linux target) and replaces both binaries in installDir, the
// directory the currently-running executable already lives in. Returns
// the (unchanged) path to the now-upgraded tubeless binary.
func applyLinux(archivePath, installDir string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("open gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("read tar: %w", err)
		}
		name := filepath.Base(hdr.Name)
		if hdr.Typeflag != tar.TypeReg || (name != "tubeless" && name != "tubeless-config") {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", hdr.Name, err)
		}
		if err := replaceFile(data, filepath.Join(installDir, name), 0o755); err != nil {
			return "", err
		}
	}
	return filepath.Join(installDir, "tubeless"), nil
}

// bundleRootFor walks Contents/MacOS/tubeless back up to the .app
// itself. Only meaningful on darwin (called from apply/restart_darwin.go
// there), but pure path-string logic — no reason to hide it behind a
// build tag too.
func bundleRootFor(execPath string) string {
	dir := filepath.Dir(execPath) // .../Tubeless.app/Contents/MacOS
	dir = filepath.Dir(dir)       // .../Tubeless.app/Contents
	dir = filepath.Dir(dir)       // .../Tubeless.app
	if !strings.HasSuffix(dir, ".app") {
		return execPath
	}
	return dir
}

// applyDarwin extracts archivePath (the darwin-<arch>.zip asset, a
// zipped Tubeless.app — see package-darwin) and replaces every file
// under bundleRoot (the currently-installed Tubeless.app, resolved from
// the running executable's path) with its counterpart from the archive.
// Returns the path to the now-upgraded tubeless binary inside it.
func applyDarwin(archivePath, bundleRoot string) (string, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", fmt.Errorf("open zip: %w", err)
	}
	defer zr.Close()

	for _, entry := range zr.File {
		rel := stripBundlePrefix(entry.Name)
		if rel == "" || entry.FileInfo().IsDir() {
			continue
		}
		if err := extractZipEntry(entry, filepath.Join(bundleRoot, rel)); err != nil {
			return "", err
		}
	}
	return filepath.Join(bundleRoot, "Contents", "MacOS", "tubeless"), nil
}

// stripBundlePrefix turns a zip entry's "Tubeless.app/Contents/..." name
// into "Contents/..." relative to whatever directory the app is actually
// installed under (its own name on disk may differ, though in practice
// it never does).
func stripBundlePrefix(name string) string {
	_, rel, found := strings.Cut(name, "/")
	if !found {
		return ""
	}
	return rel
}

func extractZipEntry(entry *zip.File, destPath string) error {
	rc, err := entry.Open()
	if err != nil {
		return fmt.Errorf("open %s: %w", entry.Name, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("read %s: %w", entry.Name, err)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	return replaceFile(data, destPath, entry.Mode())
}
