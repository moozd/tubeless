package main

import (
	"os"

	"golang.org/x/term"
)

// ---------------- terminal plumbing ----------------
//
// Raw-mode entry/exit and terminal size both go through golang.org/x/term
// rather than hand-rolled ioctl syscalls: the previous implementation used
// Linux's TCGETS/TCSETS ioctl requests and syscall.Termios directly, which
// don't exist under those names on macOS/BSD (they use TIOCGETA/TIOCSETA
// with a differently-laid-out termios struct) — x/term abstracts that
// per-platform difference so this file needs no GOOS-specific variant.

// termSize reports the PTY size in cells.
func termSize() (int, int, error) {
	return term.GetSize(int(os.Stdout.Fd()))
}

// rawTermios holds the terminal state needed to switch stdin to raw mode.
type rawTermios struct {
	fd    int
	state *term.State
}

// rawMode puts stdin into cbreak raw mode (no echo, no line buffering) and
// returns a handle whose restore() puts it back.
func rawMode(fd uintptr) (*rawTermios, error) {
	ifd := int(fd)
	state, err := term.MakeRaw(ifd)
	if err != nil {
		return nil, err
	}
	return &rawTermios{fd: ifd, state: state}, nil
}

func (r *rawTermios) restore() {
	term.Restore(r.fd, r.state)
}

// readKeys forwards every input byte to out until EOF.
func readKeys(f *os.File, out chan<- byte) {
	buf := make([]byte, 256)
	for {
		n, err := f.Read(buf)
		for i := 0; i < n; i++ {
			out <- buf[i]
		}
		if err != nil {
			return
		}
	}
}
