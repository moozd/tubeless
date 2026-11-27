package applet

import (
	"archive/tar"
	"compress/bzip2"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// CEFVersion is the CEF build applets run on. The C API version in
// pkg/applet/cef (CEF_API_VERSION) must match it.
const CEFVersion = "158.0.6+g23f1736+chromium-158.0.8127.89"

// cefBuild is one downloadable CEF "minimal" build.
type cefBuild struct {
	platform string
	sha1     string
	size     int64
}

// cefBuilds are the pinned builds, by GOOS/GOARCH. The SHA-1 sums come
// from cef-builds.spotifycdn.com/index.json.
var cefBuilds = map[string]cefBuild{
	"linux/amd64":  {"linux64", "ec558d6b418d5e31c8ee630f53c3983128d1cbe0", 339902305},
	"linux/arm64":  {"linuxarm64", "ba2969fecbb1cd305f5f3d532ee62e703e83c8ce", 435458439},
	"darwin/arm64": {"macosarm64", "63faae7ccd6a497430c06d26b17c4cac20dca387", 140851604},
	"darwin/amd64": {"macosx64", "49f270a76788af4ff8d5d6de05b1adf22377108c", 149303893},
}

const cefBuildsURL = "https://cef-builds.spotifycdn.com/"

// versionFile marks a complete runtime: it holds CEFVersion.
const versionFile = "VERSION"

// staleLock is how old a lock must be before a crashed install no
// longer blocks a new one.
const staleLock = 30 * time.Minute

// RuntimeReady reports whether dir holds a complete runtime of
// CEFVersion.
func RuntimeReady(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, versionFile))
	return err == nil && strings.TrimSpace(string(data)) == CEFVersion
}

// InstallRuntime downloads the pinned CEF build for this machine into
// dir, and writes progress lines to progress. It checks the download's
// SHA-1 before it replaces dir. Two installs at the same time download
// once: the second waits for the first.
func InstallRuntime(dir string, progress io.Writer) error {
	build, ok := cefBuilds[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return fmt.Errorf("no CEF build for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	unlock, err := lockInstall(dir)
	if err != nil {
		return err
	}
	defer unlock()
	if RuntimeReady(dir) {
		return nil
	}
	return install(dir, build, progress)
}

func install(dir string, build cefBuild, progress io.Writer) error {
	tmp := fmt.Sprintf("%s.tmp-%d", dir, os.Getpid())
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return fmt.Errorf("runtime dir: %w", err)
	}
	defer os.RemoveAll(tmp)
	if err := download(tmp, build, progress); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tmp, versionFile), []byte(CEFVersion+"\n"), 0o644); err != nil {
		return fmt.Errorf("write runtime version: %w", err)
	}
	return replaceDir(dir, tmp)
}

func buildURL(build cefBuild) string {
	name := fmt.Sprintf("cef_binary_%s_%s_minimal.tar.bz2", CEFVersion, build.platform)
	return cefBuildsURL + url.PathEscape(name)
}

// download streams the archive through SHA-1, bzip2 and tar, so no
// copy of the archive is kept on disk.
func download(dir string, build cefBuild, progress io.Writer) error {
	resp, err := http.Get(buildURL(build))
	if err != nil {
		return fmt.Errorf("download CEF: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download CEF: %s", resp.Status)
	}
	hash := sha1.New()
	counted := &progressReader{r: resp.Body, total: build.size, w: progress}
	body := io.TeeReader(counted, hash)
	if err := untar(dir, bzip2.NewReader(body)); err != nil {
		return err
	}
	if _, err := io.Copy(io.Discard, body); err != nil {
		return fmt.Errorf("download CEF: %w", err)
	}
	counted.finish()
	if sum := hex.EncodeToString(hash.Sum(nil)); sum != build.sha1 {
		return fmt.Errorf("CEF download is damaged: sha1 %s, want %s", sum, build.sha1)
	}
	return nil
}

// untar extracts the Release and Resources folders of the archive,
// flat into dir: the layout libcef expects next to itself.
func untar(dir string, r io.Reader) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("unpack CEF: %w", err)
		}
		rel, ok := runtimePath(hdr.Name)
		if !ok {
			continue
		}
		if err := extract(filepath.Join(dir, rel), hdr, tr); err != nil {
			return err
		}
	}
}

// runtimePath maps an archive path such as
// "cef_binary_.../Release/libcef.so" to "libcef.so". Paths outside
// Release and Resources, and paths that climb out, are skipped.
func runtimePath(name string) (string, bool) {
	parts := strings.SplitN(filepath.ToSlash(name), "/", 3)
	if len(parts) < 3 || (parts[1] != "Release" && parts[1] != "Resources") {
		return "", false
	}
	rel := filepath.Clean(parts[2])
	if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

func extract(path string, hdr *tar.Header, r io.Reader) error {
	switch hdr.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(path, 0o755)
	case tar.TypeSymlink:
		return os.Symlink(hdr.Linkname, path)
	case tar.TypeReg:
		return writeFile(path, os.FileMode(hdr.Mode).Perm(), r)
	}
	return nil
}

func writeFile(path string, mode os.FileMode, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("unpack CEF: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("unpack CEF: %w", err)
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return fmt.Errorf("unpack CEF %s: %w", path, err)
	}
	return f.Close()
}

// replaceDir moves the new runtime in place of dir. A helper binary in
// the old dir (from `make cef-runtime`) moves along.
func replaceDir(dir, tmp string) error {
	helper := filepath.Join(dir, "tubeless-cef-helper")
	if _, err := os.Stat(helper); err == nil {
		if err := os.Rename(helper, filepath.Join(tmp, "tubeless-cef-helper")); err != nil {
			return fmt.Errorf("keep cef helper: %w", err)
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove old runtime: %w", err)
	}
	if err := os.Rename(tmp, dir); err != nil {
		return fmt.Errorf("install runtime: %w", err)
	}
	return nil
}

// lockInstall holds dir.lock while one process installs. A second
// process waits for the lock, then finds the runtime ready.
func lockInstall(dir string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return nil, fmt.Errorf("runtime dir: %w", err)
	}
	lock := dir + ".lock"
	for {
		f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			f.Close()
			return func() { os.Remove(lock) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("lock runtime install: %w", err)
		}
		removeStaleLock(lock)
		time.Sleep(time.Second)
	}
}

func removeStaleLock(lock string) {
	st, err := os.Stat(lock)
	if err == nil && time.Since(st.ModTime()) > staleLock {
		os.Remove(lock)
	}
}

// progressReader writes "downloaded/total MB" lines as bytes pass.
type progressReader struct {
	r     io.Reader
	w     io.Writer
	total int64
	done  int64
	shown int64
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	if p.w != nil && p.done-p.shown >= p.total/100 {
		p.shown = p.done
		fmt.Fprintf(p.w, "\r  %d / %d MB", p.done>>20, p.total>>20)
	}
	return n, err
}

func (p *progressReader) finish() {
	if p.w != nil {
		fmt.Fprintf(p.w, "\r  %d / %d MB, checked\n", p.done>>20, p.total>>20)
	}
}
