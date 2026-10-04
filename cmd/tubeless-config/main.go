// Command tubeless-config is the in-terminal settings UI for tubeless. Run
// it as the terminal's app so it draws inside the SAME window:
//
//	tubeless --shell=$(which tubeless-config)
//
// It is a plain VT text-mode program (the same family as cmd/tektest), so
// everything on screen is real terminal content painted by the host's
// renderer — the config screen doubles as the renderer's test bench.
//
// The layout follows VS Code's settings: a sidebar of categories (General,
// Font, Theme, Effects, Cursor, CRT), and a pane of settings, each a title
// with its description beneath. "/" searches every setting; a dot marks
// settings changed from the factory default. Theme (colors) and Effects
// (everything the preset seeds) are independent: picking one never changes
// the other, and hand-editing a setting flips only its own axis to "custom".
//
// Edits only take effect in memory as you navigate — nothing is written to
// ~/.config/tubeless/config.toml until you press 's'. The running tubeless
// host watches that file and re-applies non-font settings live once saved,
// rebuilding its font atlas when font/atlas fields change.
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/moozd/tubeless/pkg/config"
	"github.com/moozd/tubeless/pkg/platform"
)

// version is baked in at build time via -ldflags "-X main.version=..."
// (see the Makefile's LDFLAGS) — "dev" for a plain `go build` outside it.
var version = "dev"

// escapeWait is how long a lone Escape waits for the rest of a sequence
// before it counts as the Escape key.
const escapeWait = 40 * time.Millisecond

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-version") {
		fmt.Println("tubeless-config " + version)
		return
	}

	// Fixes fc-list lookups (see pkg/font.SystemFamilies) when this binary
	// runs standalone rather than exec'd by tubeless — a macOS GUI-launched
	// process's PATH is missing whatever a login shell's profile adds.
	platform.FixEnv()

	path, err := config.DefaultPath()
	if err != nil {
		log.Fatalf("config dir: %v", err)
	}
	cfg, err := config.Load(path, "")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	cols, rows, err := termSize()
	if err != nil {
		cols, rows = 90, 30
	}

	raw, err := rawMode(os.Stdin.Fd())
	if err != nil {
		log.Fatalf("raw mode: %v", err)
	}
	defer raw.restore()

	u := newUI(cfg, path, cols, rows)
	run(u)
}

// run owns the alternate screen and the event loop: one redraw per key,
// resize, or escape timeout.
func run(u *ui) {
	fmt.Fprint(os.Stdout, "\x1b[?1049h\x1b[?25l\x1b[2J")
	defer fmt.Fprint(os.Stdout, "\x1b[?25h\x1b[?1049l")

	sigWin := make(chan os.Signal, 1)
	signal.Notify(sigWin, syscall.SIGWINCH)
	defer signal.Stop(sigWin)

	keys := make(chan byte, 64)
	go readKeys(os.Stdin, keys)

	u.redraw()
	var escTimer <-chan time.Time
	for {
		select {
		case b := <-keys:
			if u.feed(b) {
				return
			}
		case <-sigWin:
			if c, r, err := termSize(); err == nil {
				u.cols, u.rows = c, r
			}
		case <-escTimer:
			u.cancelEscape()
		}
		escTimer = nil
		if len(u.esc) == 1 {
			escTimer = time.After(escapeWait)
		}
		u.redraw()
	}
}
