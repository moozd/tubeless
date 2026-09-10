// Package platform holds OS-specific process-environment fixes.
package platform

import (
	"fmt"
	"os"
	"os/exec"
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
	if err != nil || loginPath == "" {
		return
	}
	os.Setenv("PATH", mergePath(os.Getenv("PATH"), loginPath))
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
