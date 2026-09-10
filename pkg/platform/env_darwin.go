// Package platform holds OS-specific process-environment fixes.
package platform

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ResolveLoginPath returns the PATH a login shell would see. macOS GUI apps
// (launched from Finder/Dock/launchd) inherit a minimal environment — none
// of the PATH additions a user's .zprofile/.bash_profile makes (Homebrew's
// shellenv chief among them) are present, because those files are only
// sourced by a login shell. Terminal.app/iTerm always launch one; this
// recovers the same PATH by asking the user's actual shell for it.
func ResolveLoginPath() (string, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	out, err := exec.Command(shell, "-l", "-c", "echo -n $PATH").Output()
	if err != nil {
		return "", fmt.Errorf("resolve login PATH via %s: %w", shell, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// FixEnv replaces the process's PATH with the login shell's PATH, merged
// with whatever PATH the process already had (deduplicated, existing
// entries first) so nothing already visible is lost. Must run before any
// exec.Command call (fc-list, the pty child shell, ...) that depends on a
// complete PATH. A failure to resolve the login PATH is non-fatal — the
// process just keeps its launchd-provided PATH.
func FixEnv() {
	loginPath, err := ResolveLoginPath()
	if err == nil && loginPath != "" {
		os.Setenv("PATH", mergePath(os.Getenv("PATH"), loginPath))
	}
	ensureCLIOnPath()
}

// ensureCLIOnPath symlinks the running app's CLI binaries into
// ~/.local/bin and bootstraps ~/.zprofile with that dir if needed.
// install.sh and `make install-darwin` already do this once at install
// time, but a manual reinstall — dragging a freshly downloaded
// Tubeless.app over the old one in ~/Applications — never runs either,
// so PATH would otherwise silently go stale on every such update. Runs
// on every launch since it's cheap and fully idempotent; failures are
// logged, never fatal — a broken PATH symlink shouldn't stop the app
// from starting.
func ensureCLIOnPath() {
	exe, err := os.Executable()
	if err != nil || !strings.Contains(exe, ".app/Contents/MacOS/") {
		return // not running from an installed .app bundle (e.g. a dev build)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		log.Printf("ensure PATH: mkdir %s: %v", binDir, err)
		return
	}
	dir := filepath.Dir(exe)
	symlinkInto(binDir, filepath.Join(dir, "tubeless"))
	symlinkInto(binDir, filepath.Join(dir, "tubeless-config"))
	bootstrapZprofile(home, binDir)
}

// symlinkInto (re)links binDir/<base of target> -> target, skipping the
// work entirely once the link is already correct.
func symlinkInto(binDir, target string) {
	if _, err := os.Stat(target); err != nil {
		return // sibling binary doesn't exist in this bundle
	}
	link := filepath.Join(binDir, filepath.Base(target))
	if existing, err := os.Readlink(link); err == nil && existing == target {
		return
	}
	os.Remove(link)
	if err := os.Symlink(target, link); err != nil {
		log.Printf("ensure PATH: symlink %s: %v", link, err)
	}
}

// bootstrapZprofile appends binDir to PATH in ~/.zprofile, once — macOS
// doesn't put ~/.local/bin on PATH by default the way Linux distros
// commonly do, and .zprofile (a login shell's profile) is where a login
// shell's PATH setup belongs, matching startShell's own use of -l.
func bootstrapZprofile(home, binDir string) {
	path := filepath.Join(home, ".zprofile")
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), binDir) {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("ensure PATH: open %s: %v", path, err)
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "\n# Added by tubeless\nexport PATH=\"%s:$PATH\"\n", binDir)
}

func mergePath(current, extra string) string {
	seen := make(map[string]bool)
	var merged []string
	for _, dir := range strings.Split(current, ":") {
		if dir != "" && !seen[dir] {
			seen[dir] = true
			merged = append(merged, dir)
		}
	}
	for _, dir := range strings.Split(extra, ":") {
		if dir != "" && !seen[dir] {
			seen[dir] = true
			merged = append(merged, dir)
		}
	}
	return strings.Join(merged, ":")
}
