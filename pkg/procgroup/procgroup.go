// Package procgroup runs an exec.Cmd in its own process group and gives
// callers the signal-then-escalate teardown pattern pkg/ptyio's
// Session.Close proved out first: send a signal to the whole group, wait
// a bounded grace period, then SIGKILL the group if it hasn't exited.
// ptyio's own Close keeps its extra PTY-specific step (also signaling
// whichever job currently owns the terminal in the foreground, which can
// differ from cmd's own group) layered on top of these primitives rather
// than duplicated here — see its own doc comment.
package procgroup

import (
	"os/exec"
	"syscall"
	"time"
)

// Setup configures cmd to start in a new process group (setpgid) so its
// entire subtree — not just the direct child — can be signaled together.
// Call this before cmd.Start()/cmd.Run().
func Setup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// Signal sends sig to cmd's process group, falling back to just the
// direct process if the group lookup fails (e.g. it already exited).
// cmd.Process must be non-nil (i.e. Start already succeeded).
func Signal(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd.Process == nil {
		return
	}
	if pgid, err := syscall.Getpgid(cmd.Process.Pid); err == nil {
		syscall.Kill(-pgid, sig)
	} else {
		cmd.Process.Signal(sig)
	}
}

// Watched owns the single permitted Wait() call for a started *exec.Cmd
// and lets any number of goroutines observe its exit without contention:
// unlike a channel's single buffered value (which only one receiver ever
// gets), a closed channel broadcasts losslessly to every receiver — which
// is what lets both "did the process exit on its own" detection and
// Close's own "block until it's actually gone" observe the very same
// exit safely.
type Watched struct {
	cmd  *exec.Cmd
	Done chan struct{} // closed exactly once, when cmd.Wait() returns
	Err  error         // valid only once Done is closed
}

// Watch starts the one goroutine allowed to call cmd.Wait() for cmd —
// call this exactly once, right after cmd.Start() succeeds. Calling
// cmd.Wait() yourself afterward (or calling Watch twice on the same cmd)
// is always wrong: Wait may only be called once per process.
func Watch(cmd *exec.Cmd) *Watched {
	w := &Watched{cmd: cmd, Done: make(chan struct{})}
	go func() {
		w.Err = cmd.Wait()
		close(w.Done)
	}()
	return w
}

// Close is the full teardown sequence: SIGTERM the group, wait up to
// grace, escalate to SIGKILL against the same group if it's still
// running, then block until Done actually closes. Safe to call even if
// the process already exited on its own (Done already closed) — this
// returns immediately in that case rather than sending pointless signals.
func (w *Watched) Close(grace time.Duration) {
	select {
	case <-w.Done:
		return
	default:
	}
	Signal(w.cmd, syscall.SIGTERM)
	select {
	case <-w.Done:
		return
	case <-time.After(grace):
	}
	Signal(w.cmd, syscall.SIGKILL)
	<-w.Done
}
