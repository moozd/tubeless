package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// maxSessionLogBytes is the size past which startSessionLog rotates the
// log to a single ".1" backup, so a long-lived install never grows it
// without bound.
const maxSessionLogBytes = 5 << 20

// sessionLogPath is where the GUI writes its log and crash traces:
// ~/Library/Logs/tubeless on macOS (where Console.app looks), the XDG
// state dir everywhere else.
func sessionLogPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Logs", "tubeless", "tubeless.log"), nil
	}
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "tubeless", "tubeless.log"), nil
}

// startSessionLog sends the standard logger, the Go runtime's crash
// output and native stderr to a log file. An app launched from the
// Finder/Dock has its stderr going nowhere, so without this a panic in
// any goroutine (exit code 2) closes the window and leaves no trace.
func startSessionLog() {
	path, err := sessionLogPath()
	if err != nil {
		log.Printf("session log disabled: %v", err)
		return
	}
	f, err := openSessionLog(path)
	if err != nil {
		log.Printf("session log disabled: %v", err)
		return
	}
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	debug.SetTraceback("all")
	if isStderrRedirected(f) {
		log.SetOutput(os.Stderr)
	} else {
		log.SetOutput(io.MultiWriter(os.Stderr, f))
		setCrashOutput(f)
	}
	log.Printf("start: tubeless %s pid=%d args=%q", version, os.Getpid(), os.Args[1:])
}

// isStderrRedirected points fd 2 at f when stderr is not a terminal,
// so the Go runtime's crash trace and native output both land in the
// log. A native abort in AppKit/GLFW writes its reason (an uncaught
// exception, a failed assert) to fd 2 and nowhere else: Go's trace has
// no C frames, and the .ips report macOS writes after it has no native
// stack either, since the runtime raises the signal again from its own
// handler.
func isStderrRedirected(f *os.File) bool {
	if term.IsTerminal(int(os.Stderr.Fd())) {
		return false
	}
	if err := unix.Dup2(int(f.Fd()), int(os.Stderr.Fd())); err != nil {
		log.Printf("redirect stderr to session log: %v", err)
		return false
	}
	return true
}

// setCrashOutput copies the Go runtime's crash trace to f, next to the
// copy on stderr.
func setCrashOutput(f *os.File) {
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		log.Printf("set crash output: %v", err)
	}
}

// openSessionLog opens path for appending, creating its directory and
// rotating an oversized previous log first.
func openSessionLog(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	if info, err := os.Stat(path); err == nil && info.Size() > maxSessionLogBytes {
		if err := os.Rename(path, path+".1"); err != nil {
			log.Printf("rotate session log: %v", err)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return f, nil
}
