// runSSHIntegration implements `tubeless ssh-integration <install|remove|status>`:
// a managed block in ~/.ssh/config that makes plain `ssh` transparently
// install tubeless's own terminfo entry on a remote host before handing
// off to the interactive shell.
//
// The bug this fixes: a tubeless session runs with TERM=tubeless (see
// pkg/ptyio/terminfo.go), and ssh forwards that TERM value to the remote
// as part of pty allocation — but the compiled terminfo entry itself
// lives only in this machine's per-user cache dir (TERMINFO_DIRS), which
// ssh does not forward. The remote's ncurses can't resolve "tubeless" at
// all, and whatever falls out of that (a hard failure, or a silent
// fallback with wrong capabilities) is what shows up as garbled output.
//
// This is done via ssh_config's own Match/RemoteCommand mechanism rather
// than wrapping the ssh binary or parsing its argv: RemoteCommand is the
// extension point OpenSSH already provides for "run this, then become my
// shell", so there's no argument-parsing edge case to get wrong for every
// flag/ProxyJump/multiplexing combination a real ssh invocation might use.
// Match exec gates the block to sessions actually launched from inside
// tubeless (TERM=tubeless in the parent environment) — an ssh run from
// any other terminal is untouched.
package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/moozd/tubeless/pkg/ptyio"
)

const (
	sshIntegrationBegin = "# >>> tubeless ssh integration >>>"
	sshIntegrationEnd   = "# <<< tubeless ssh integration <<<"
)

// runSSHIntegration is main's entry point for the `ssh-integration`
// subcommand. It never returns normally — it always os.Exit's.
func runSSHIntegration(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: tubeless ssh-integration <install|remove|status>")
		os.Exit(2)
	}

	configPath, err := sshConfigPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tubeless ssh-integration:", err)
		os.Exit(1)
	}

	switch args[0] {
	case "install":
		os.Exit(runSSHIntegrationInstall(configPath))
	case "remove":
		os.Exit(runSSHIntegrationRemove(configPath))
	case "status":
		os.Exit(runSSHIntegrationStatus(configPath))
	default:
		fmt.Fprintln(os.Stderr, "usage: tubeless ssh-integration <install|remove|status>")
		os.Exit(2)
	}
}

func sshConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".ssh", "config"), nil
}

func runSSHIntegrationInstall(configPath string) int {
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "tubeless ssh-integration:", err)
		return 1
	}
	existing, err := readIfExists(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tubeless ssh-integration:", err)
		return 1
	}

	// The block must precede any Host/Match entry the user already has —
	// ssh_config keeps the first value it sees for most keywords, so a
	// block appended at the end would silently never apply to a Host
	// section defined above it.
	updated := sshIntegrationBlock() + "\n" + stripManagedBlock(existing)
	if err := os.WriteFile(configPath, []byte(updated), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "tubeless ssh-integration:", err)
		return 1
	}
	fmt.Println("tubeless ssh-integration: installed into", configPath)
	return 0
}

func runSSHIntegrationRemove(configPath string) int {
	existing, err := readIfExists(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tubeless ssh-integration:", err)
		return 1
	}
	updated := stripManagedBlock(existing)
	if updated == existing {
		fmt.Println("tubeless ssh-integration: not installed in", configPath)
		return 0
	}
	if err := os.WriteFile(configPath, []byte(updated), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "tubeless ssh-integration:", err)
		return 1
	}
	fmt.Println("tubeless ssh-integration: removed from", configPath)
	return 0
}

func runSSHIntegrationStatus(configPath string) int {
	existing, err := readIfExists(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tubeless ssh-integration:", err)
		return 1
	}
	if strings.Contains(existing, sshIntegrationBegin) {
		fmt.Println("tubeless ssh-integration: installed in", configPath)
		return 0
	}
	fmt.Println("tubeless ssh-integration: not installed")
	return 1
}

func readIfExists(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(b), nil
}

// stripManagedBlock removes a previously installed block (markers
// included) from config, leaving everything else untouched — install
// calls this before re-inserting a fresh copy, so re-running install is
// idempotent instead of accumulating duplicate blocks.
func stripManagedBlock(config string) string {
	start := strings.Index(config, sshIntegrationBegin)
	if start < 0 {
		return config
	}
	end := strings.Index(config, sshIntegrationEnd)
	if end < 0 {
		return config
	}
	end += len(sshIntegrationEnd)
	for end < len(config) && config[end] == '\n' {
		end++
	}
	return config[:start] + config[end:]
}

// sshIntegrationBlock builds the managed ssh_config block: a Match gated
// on the local TERM being tubeless's own, and a RemoteCommand that
// installs this same terminfo entry on the remote (if it isn't there
// already) before exec'ing into the login shell.
//
// The remote script is handed to ssh_config as one line — RemoteCommand's
// value "extends to the end of the line" (ssh_config(5)) — and reaches
// the remote through ssh's own %-token expansion, which treats a bare
// "%s" as an unrecognized token and refuses to parse the whole file; the
// printf format string is written as "%%s" so it survives that expansion
// as a literal "%s" by the time the remote shell sees it.
func sshIntegrationBlock() string {
	term := ptyio.TerminfoTerm()
	payload := base64.StdEncoding.EncodeToString(ptyio.TerminfoSource())

	remoteCmd := `infocmp ` + term + ` >/dev/null 2>&1 || { command -v tic >/dev/null 2>&1 && command -v base64 >/dev/null 2>&1 && printf %%s "` + payload + `" | base64 -d | tic -x -o ~/.terminfo - >/dev/null 2>&1; }; exec "${SHELL:-/bin/sh}" -l`
	matchLine := `Match exec "test \"$TERM\" = ` + term + `"`

	return strings.Join([]string{
		sshIntegrationBegin,
		"# Installs tubeless's own terminfo entry on the remote host (if",
		"# missing) before handing off to your login shell, so TERM=" + term,
		"# resolves the same way it does locally instead of leaving the",
		"# remote's ncurses unable to find it. Only applies to sessions",
		"# started from inside tubeless (TERM=" + term + " locally) — any",
		"# other terminal is untouched.",
		"#",
		"# Managed by `tubeless ssh-integration` — edits here are",
		"# overwritten by install/remove.",
		matchLine,
		"    RequestTTY yes",
		"    RemoteCommand " + remoteCmd,
		sshIntegrationEnd,
		"",
	}, "\n")
}
