package procgroup

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestCloseExitsPromptlyOnSIGTERM(t *testing.T) {
	cmd := exec.Command("sh", "-c", "trap 'exit 0' TERM; sleep 5")
	Setup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	w := Watch(cmd)
	// Let the trap install before Close signals — a signal that arrives
	// before the shell's trap builtin runs uses the default TERM
	// disposition (still exits, just not via the intended path this test
	// wants to exercise), so this is a smoke test on timing, not strict.
	time.Sleep(50 * time.Millisecond)

	start := time.Now()
	w.Close(2 * time.Second)
	elapsed := time.Since(start)
	if w.Err != nil {
		if exitErr, ok := w.Err.(*exec.ExitError); !ok || exitErr.ExitCode() != 0 {
			t.Fatalf("Close returned unexpected error: %v", w.Err)
		}
	}
	if elapsed > time.Second {
		t.Fatalf("Close took %v — expected a prompt exit well under the 2s grace", elapsed)
	}
}

func TestCloseEscalatesToSIGKILL(t *testing.T) {
	cmd := exec.Command("sh", "-c", "trap '' TERM; sleep 5")
	Setup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	w := Watch(cmd)
	time.Sleep(50 * time.Millisecond)

	start := time.Now()
	w.Close(300 * time.Millisecond)
	elapsed := time.Since(start)
	if elapsed < 300*time.Millisecond {
		t.Fatalf("Close returned in %v — shouldn't beat its own grace period", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Close took %v after escalation — SIGKILL should be prompt", elapsed)
	}
	exitErr, ok := w.Err.(*exec.ExitError)
	if !ok {
		t.Fatalf("got %v (%T), want *exec.ExitError from SIGKILL", w.Err, w.Err)
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("got exit status %v, want signaled by SIGKILL", exitErr.Sys())
	}
}

func TestCloseAlreadyExited(t *testing.T) {
	cmd := exec.Command("true")
	Setup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	w := Watch(cmd)
	<-w.Done // let it exit on its own first

	start := time.Now()
	w.Close(time.Second) // must return immediately, not send pointless signals or hang
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("Close on an already-exited process took %v, want near-instant", elapsed)
	}
}

// TestCloseObservableByMultipleReaders is the regression case for the
// bug caught in the real tubeless open smoke test: a top-level watcher
// (detecting a process exited on its own) and Close's own "block until
// gone" must both be able to observe the same exit — Done is a closed
// channel precisely so both can receive from it, unlike a single-value
// buffered channel where only one reader would ever get the value.
func TestCloseObservableByMultipleReaders(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 0")
	Setup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	w := Watch(cmd)

	sawIt := make(chan bool, 1)
	go func() {
		select {
		case <-w.Done:
			sawIt <- true
		case <-time.After(2 * time.Second):
			sawIt <- false
		}
	}()

	w.Close(time.Second) // the second reader

	select {
	case ok := <-sawIt:
		if !ok {
			t.Fatalf("the independent watcher never observed the exit")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("independent watcher goroutine never reported back")
	}
}
