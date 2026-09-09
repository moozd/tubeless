package main

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/moozd/tubeless/pkg/ptyio"
	"github.com/moozd/tubeless/pkg/screen"
)

// TestPtyCoordinatorSetsCloseRequestedOnShellExit locks in the fix for the
// zombie-process bug: when the shell exits, ptyCoordinator must set
// closeRequested so runLoop actually exits and the deferred sess.Close()
// (which reaps the shell) runs, instead of the window sitting open with the
// shell left unreaped indefinitely.
func TestPtyCoordinatorSetsCloseRequestedOnShellExit(t *testing.T) {
	sess, err := ptyio.Start("/bin/sh", []string{"-c", "exit 0"}, cols, rows)
	if err != nil {
		t.Fatalf("start shell: %v", err)
	}
	defer sess.Close()

	var shared atomic.Pointer[screen.Screen]
	shared.Store(screen.New(cols, rows))
	resizeCh := make(chan resizeReq, 1)
	closeRequested := new(atomic.Bool)

	done := make(chan struct{})
	go func() {
		ptyCoordinator(sess, &shared, resizeCh, 0, closeRequested)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ptyCoordinator did not return after the shell exited")
	}
	if !closeRequested.Load() {
		t.Fatal("closeRequested was not set after the shell exited")
	}
}
