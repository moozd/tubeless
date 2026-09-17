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
// underline color) — so ncurses-based apps (neovim, lazygit) can discover
// real undercurl style+color support the normal terminfo-lookup way.
// Neovim has no manual termcap-override mechanism the way Vim's t_Cs/
// t_AU are (it reads terminfo only), so without an entry that actually
// advertises these, there is no per-app workaround available at all —
// this is the only lever.
//
//go:embed tubeless.terminfo
var tubelessTerminfoSource []byte

// terminfoTerm is compileTerminfo's TERM name — a real terminfo entry, not
// a name borrowed from another emulator, so `echo $TERM` in a tubeless
// session unambiguously names its own capabilities.
const terminfoTerm = "tubeless"

// augmentedTerms are TERM names tubeless also patches with Smulx/Setulc,
// alongside its own "tubeless" entry above: a program running inside tmux
// or screen sees TERM=tmux-256color / screen-256color — the multiplexer's
// own name, not tubeless's — since that's what it re-exports to every
// pane regardless of what the outer terminal calls itself. tubeless's
// TERM=tubeless never reaches those programs at all, so without patching
// these two names too, undercurl style/color silently degrades to a
// plain underline the instant anything runs inside a multiplexer.
var augmentedTerms = []string{"tmux-256color", "screen-256color"}

// undercurlCaps is appended to a system terminfo entry's own infocmp
// output to add undercurl style/color — the same two capabilities
// tubeless.terminfo declares directly, in the same wire format (see its
// own doc comment) — to build augmentTerminfo's derived entries.
const undercurlCaps = `
	Smulx=\E[4\:%p1%dm,
	Setulc=\E[58\:2\:\:%p1%{65536}%/%d\:%p1%{256}%/%{255}%&%d\:%p1%{255}%&%dm,
`

// setupTerminfo compiles tubelessTerminfoSource, plus an augmented copy of
// each augmentedTerms entry, into a per-user cache directory, and returns
// the TERM/TERMINFO_DIRS the child process needs to pick all of it up.
// Recompiling on every Start is deliberate over caching compiled copies
// across runs — tic on entries this size is sub-millisecond, far cheaper
// than the staleness bugs a cache invalidation scheme would risk if these
// sources ever change between tubeless versions, or the system's own
// tmux-256color/screen-256color entries change underneath a stale cache.
// Falls back to plain xterm-256color (undercurl style/color simply
// unadvertised, as it always was until now) if tic isn't installed or
// compiling the "tubeless" entry itself fails — a missing terminfo
// compiler shouldn't block launching a shell. A single augmentedTerms
// entry failing (no system tmux-256color installed, say) is not fatal to
// the rest — it's just not patched, logged and skipped.
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
	for _, name := range augmentedTerms {
		if err := augmentTerminfo(dir, name); err != nil {
			log.Printf("tubeless: patching %s terminfo: %v — undercurl style/color won't be advertised inside it", name, err)
		}
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
	return ticCompile(dir, src)
}

// augmentTerminfo derives a copy of the system's own `name` terminfo
// entry (via infocmp) with undercurlCaps appended, and compiles it into
// dir under that same name — so a lookup for TERM=name finds real
// undercurl support too, without needing a hand-maintained copy of
// whatever that system entry currently contains (see augmentedTerms).
func augmentTerminfo(dir, name string) error {
	base, err := exec.Command("infocmp", "-x", name).Output()
	if err != nil {
		return fmt.Errorf("infocmp %s: %w", name, err)
	}
	src := filepath.Join(dir, name+".terminfo")
	if err := os.WriteFile(src, append(base, undercurlCaps...), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", src, err)
	}
	return ticCompile(dir, src)
}

// ticCompile is compileTerminfo/augmentTerminfo's shared final step: -x
// accepts Smulx/Setulc as user-defined extended capabilities rather than
// warning/rejecting them as unrecognized.
func ticCompile(dir, src string) error {
	if out, err := exec.Command("tic", "-x", "-o", dir, src).CombinedOutput(); err != nil {
		return fmt.Errorf("tic %s: %w: %s", src, err, out)
	}
	return nil
}
