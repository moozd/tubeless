package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
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

// startSessionLog tees the standard logger into a log file and points
// the Go runtime's crash output at it too. An app launched from the
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
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		log.Printf("set crash output: %v", err)
	}
	// A native abort (SIGABRT in AppKit/GLFW) has no C stack in Go's
	// trace. "crash" re-raises the signal after the trace, so macOS
	// writes an .ips report with the ObjC stack and abort reason.
	debug.SetTraceback("crash")
	log.Printf("start: tubeless %s pid=%d args=%q", version, os.Getpid(), os.Args[1:])
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
