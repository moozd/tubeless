// Package ptyio spawns a child process attached to a pseudo-terminal.
package ptyio

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

// Session is a running child process plus its PTY master end.
type Session struct {
	Master *os.File
	cmd    *exec.Cmd
}

// Start launches name(args...) attached to a new PTY sized cols x rows.
func Start(name string, args []string, cols, rows int) (*Session, error) {
	cmd := exec.Command(name, args...)
	// xterm-256color, not something VT340-accurate: the terminfo for a
	// real VT340 declares only what a genuine 1980s serial terminal
	// could do — no alternate screen buffer, no 256/true color, and
	// (concretely, this is what broke modern TUI apps here) real
	// padding-delay directives like flash's "$<200/>", meant for a slow
	// physical terminal that needs literal wait time between writes.
	// ncurses-based apps (neovim, lazygit) honor whatever the terminfo
	// for the declared TERM says, so they were emitting that padding
	// syntax as real output, and avoiding capabilities (alt-screen
	// among them) the entry doesn't advertise — independent of what the
	// emulator itself actually implements. xterm-256color is the
	// universally-supported baseline every terminfo database has.
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	master, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, fmt.Errorf("start pty for %s: %w", name, err)
	}
	return &Session{Master: master, cmd: cmd}, nil
}

// Resize updates the PTY's reported window size.
func (s *Session) Resize(cols, rows int) error {
	return pty.Setsize(s.Master, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// Write sends bytes to the child (e.g. keyboard input).
func (s *Session) Write(p []byte) (int, error) {
	return s.Master.Write(p)
}

// Close terminates the PTY master and waits for the child to exit.
func (s *Session) Close() error {
	s.Master.Close()
	return s.cmd.Wait()
}
