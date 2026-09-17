package ptyio

import (
	_ "embed"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

// tubelessTerminfoSource is a terminfo entry for TERM=tubeless: xterm-
// 256color (see Start's own doc comment for why that base, not something
// VT340-accurate) extended with the two capabilities its system terminfo
// lacks — Smulx (styled/curly underline) and Setulc (an independent
// underline color) — so ncurses-based apps (neovim, tmux, lazygit) can
// discover real undercurl style+color support the normal terminfo-lookup
// way, instead of every app/user needing its own manual termcap override
// (t_Cs/t_AU in Neovim, terminal-overrides in tmux) to get it.
//
//go:embed tubeless.terminfo
var tubelessTerminfoSource []byte

// terminfoTerm is compileTerminfo's TERM name — a real terminfo entry, not
// a name borrowed from another emulator, so `echo $TERM` in a tubeless
// session unambiguously names its own capabilities.
const terminfoTerm = "tubeless"

// setupTerminfo compiles tubelessTerminfoSource into a per-user cache
// directory and returns the TERM/TERMINFO_DIRS the child process needs to
// pick it up. Recompiling on every Start is deliberate over caching a
// compiled copy across runs — tic on a five-line source is sub-
// millisecond, far cheaper than the staleness bugs a cache invalidation
// scheme would risk if this source ever changes between tubeless
// versions. Falls back to plain xterm-256color (undercurl style/color
// simply unadvertised, as it always was until now) if tic isn't
// installed or compilation fails for any reason — a missing terminfo
// compiler shouldn't block launching a shell.
func setupTerminfo() (term string, extraEnv []string) {
	dir, err := terminfoCacheDir()
	if err != nil {
		log.Printf("tubeless: resolving terminfo cache dir: %v — undercurl style/color won't be advertised", err)
		return "xterm-256color", nil
	}
	if err := compileTerminfo(dir); err != nil {
		log.Printf("tubeless: compiling terminfo: %v — undercurl style/color won't be advertised", err)
		return "xterm-256color", nil
	}
	// A trailing empty entry in TERMINFO_DIRS (the trailing ':') is
	// ncurses' own convention for "then fall back to the compiled-in
	// default search path" — so anything else a child looks up by TERM
	// still resolves normally against the system database.
	return terminfoTerm, []string{"TERMINFO_DIRS=" + dir + ":"}
}

func terminfoCacheDir() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("user cache dir: %w", err)
	}
	return filepath.Join(cache, "tubeless", "terminfo"), nil
}

func compileTerminfo(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	src := filepath.Join(dir, "tubeless.terminfo")
	if err := os.WriteFile(src, tubelessTerminfoSource, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", src, err)
	}
	// -x: accept Smulx/Setulc as user-defined extended capabilities
	// rather than warning/rejecting them as unrecognized.
	if out, err := exec.Command("tic", "-x", "-o", dir, src).CombinedOutput(); err != nil {
		return fmt.Errorf("tic: %w: %s", err, out)
	}
	return nil
}
