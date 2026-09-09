// Package ptyio spawns a child process attached to a pseudo-terminal.
package ptyio

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
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

// closeGrace is how long Close waits for the child to exit on its own
// (via the PTY hangup its master-close below triggers, or the explicit
// SIGHUP) before escalating to SIGKILL.
const closeGrace = 2 * time.Second

// Close terminates the PTY master and waits for the child to exit.
// Closing the master alone should already deliver a SIGHUP to the
// child's foreground process group (a normal PTY hangup) — but not every
// shell/program reliably exits on that alone (one that's stopped, or a
// runaway grandchild still holding the PTY's slave end open), so this
// also signals the whole process group directly and, if the child still
// hasn't exited after closeGrace, escalates to SIGKILL against the shell's
// own group and whichever job (e.g. a stuck nvim) currently owns the
// terminal in the foreground, rather than letting the caller's shutdown
// hang indefinitely on cmd.Wait().
//
// This deliberately does not sweep the whole session: a backgrounded,
// nohup'd, or disown'd job is left running, matching normal shell/Unix
// semantics — only the shell and whatever it's currently running in the
// foreground are guaranteed to die with the terminal.
func (s *Session) Close() error {
	// Read the foreground process group before closing Master: once closed,
	// there's nothing left to query it from.
	fgPgid, _ := s.foregroundPgid()
	s.Master.Close()
	if s.cmd.Process != nil {
		if pgid, err := syscall.Getpgid(s.cmd.Process.Pid); err == nil {
			syscall.Kill(-pgid, syscall.SIGHUP)
		} else {
			s.cmd.Process.Signal(syscall.SIGHUP)
		}
	}

	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(closeGrace):
		s.killEscalate(fgPgid)
		return <-done
	}
}

// foregroundPgid returns whichever process group currently owns the PTY in
// the foreground (e.g. a running nvim).
func (s *Session) foregroundPgid() (int, error) {
	return unix.IoctlGetInt(int(s.Master.Fd()), unix.TIOCGPGRP)
}

// killEscalate is Close's last resort once closeGrace has elapsed and the
// child still hasn't exited on the SIGHUP alone: SIGKILL the shell's own
// process group, whichever group was in the foreground when Close started
// (covers a job that ignores/traps SIGHUP, or was stopped), and finally the
// shell PID itself as a catch-all.
func (s *Session) killEscalate(fgPgid int) {
	if s.cmd.Process == nil {
		return
	}
	if pgid, err := syscall.Getpgid(s.cmd.Process.Pid); err == nil {
		syscall.Kill(-pgid, syscall.SIGKILL)
	}
	if fgPgid > 0 {
		syscall.Kill(-fgPgid, syscall.SIGKILL)
	}
	s.cmd.Process.Kill()
}
