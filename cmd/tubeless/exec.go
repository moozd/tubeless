// runExec implements `tubeless exec <applet> [args...]`: it opens an
// applet page in the current pane. The page runs in the tubeless window
// (see applets.go); this process holds the pane. It fills the pane with
// placeholder cells so the window knows where to paint, reports pane
// size and focus changes, and exits with the applet's exit code.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/moozd/tubeless/pkg/applet"
	"github.com/moozd/tubeless/pkg/screen"
)

// execConnectWait is how long exec waits for the window to answer the
// open request before it decides the window has no applet support.
const execConnectWait = 5 * time.Second

const (
	// Alternate screen, hidden cursor, focus reports on.
	execModesOn  = "\x1b[?1049h\x1b[?25l\x1b[?1004h"
	execModesOff = "\x1b[?1004l\x1b[?25h\x1b[0m\x1b[?1049l"
	focusIn      = "\x1b[I"
	focusOut     = "\x1b[O"
	ctrlC        = 0x03
)

func runExec(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: tubeless exec <applet> [args...]")
		os.Exit(2)
	}
	code, output, err := execApplet(args[0], args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "tubeless exec:", err)
	}
	if output != "" {
		fmt.Print(output)
	}
	os.Exit(code)
}

// execSession is one running `tubeless exec`.
type execSession struct {
	id       int
	fd       int
	conn     net.Conn
	messages chan applet.Message
}

func execApplet(name string, args []string) (int, string, error) {
	fd := int(os.Stdin.Fd())
	cols, rows, err := term.GetSize(fd)
	if err != nil {
		return 1, "", fmt.Errorf("read pane size: %w", err)
	}
	cols, rows = min(cols, screen.AppletMaxCols), min(rows, screen.AppletMaxRows)
	ln, sock, err := listenExecSocket()
	if err != nil {
		return 1, "", err
	}
	defer os.Remove(sock)
	defer ln.Close()

	saved, err := term.MakeRaw(fd)
	if err != nil {
		return 1, "", fmt.Errorf("raw mode: %w", err)
	}
	defer restoreExecPane(fd, saved)

	s := &execSession{id: rand.IntN(screen.AppletMaxID), fd: fd}
	if err := s.sendOpen(name, args, sock, cols, rows); err != nil {
		return 1, "", err
	}
	paintPlaceholders(s.id, cols, rows)
	if err := s.accept(ln); err != nil {
		return 1, "", err
	}
	defer s.conn.Close()
	return s.run()
}

// listenExecSocket opens the socket the window dials back on.
func listenExecSocket() (net.Listener, string, error) {
	dir := filepath.Join(execRuntimeDir(), "tubeless")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, "", fmt.Errorf("socket dir: %w", err)
	}
	sock := filepath.Join(dir, "exec-"+strconv.Itoa(os.Getpid())+".sock")
	if err := os.Remove(sock); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, "", fmt.Errorf("remove stale socket: %w", err)
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return nil, "", fmt.Errorf("listen %s: %w", sock, err)
	}
	return ln, sock, nil
}

func execRuntimeDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return dir
	}
	return os.TempDir()
}

func restoreExecPane(fd int, saved *term.State) {
	fmt.Fprint(os.Stdout, execModesOff)
	if err := term.Restore(fd, saved); err != nil {
		fmt.Fprintln(os.Stderr, "tubeless exec: restore terminal:", err)
	}
}

func (s *execSession) sendOpen(name string, args []string, sock string, cols, rows int) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("working directory: %w", err)
	}
	req := applet.Request{ID: s.id, Socket: sock, Name: name, Args: args, Cwd: cwd, Cols: cols, Rows: rows}
	osc, err := applet.EncodeRequest(req, os.Getenv("TMUX") != "")
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(os.Stdout, execModesOn+osc)
	return err
}

// accept waits for the window to dial back, then for its first answer.
func (s *execSession) accept(ln net.Listener) error {
	if err := ln.(*net.UnixListener).SetDeadline(time.Now().Add(execConnectWait)); err != nil {
		return fmt.Errorf("socket deadline: %w", err)
	}
	conn, err := ln.Accept()
	if err != nil {
		return errors.New("no answer from the window; run it in tubeless built with applet support (make tubeless-cef)")
	}
	s.conn = conn
	s.messages = make(chan applet.Message, 16)
	go readExecMessages(conn, s.messages)
	return nil
}

func readExecMessages(conn net.Conn, out chan<- applet.Message) {
	if err := applet.ReadMessages(conn, out); err != nil {
		fmt.Fprintln(os.Stderr, "tubeless exec:", err)
	}
}

// run serves the pane until the applet ends.
func (s *execSession) run() (int, string, error) {
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	stdin := pumpExecStdin()
	for {
		select {
		case m, ok := <-s.messages:
			if !ok {
				return 1, "", errors.New("the window closed the applet")
			}
			if r, done := s.handleMessage(m); done {
				return r.code, r.output, r.err
			}
		case b, ok := <-stdin:
			if !ok || bytes.IndexByte(b, ctrlC) >= 0 {
				s.send(applet.Message{Type: applet.MsgClose})
				return 130, "", nil
			}
			s.forwardFocus(b)
		case <-winch:
			s.resize()
		}
	}
}

// execResult is how `tubeless exec` ends.
type execResult struct {
	code   int
	output string
	err    error
}

func (s *execSession) handleMessage(m applet.Message) (execResult, bool) {
	switch m.Type {
	case applet.MsgExit:
		return execResult{code: m.Code, output: m.Output}, true
	case applet.MsgError:
		return execResult{code: 1, err: errors.New(m.Error)}, true
	}
	return execResult{}, false
}

// forwardFocus relays tmux's focus reports (mode 1004) for this pane.
func (s *execSession) forwardFocus(b []byte) {
	text := string(b)
	if strings.Contains(text, focusIn) {
		s.send(applet.Message{Type: applet.MsgFocus, Focused: true})
	}
	if strings.Contains(text, focusOut) {
		s.send(applet.Message{Type: applet.MsgFocus, Focused: false})
	}
}

func (s *execSession) resize() {
	cols, rows, err := term.GetSize(s.fd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tubeless exec: read pane size:", err)
		return
	}
	cols, rows = min(cols, screen.AppletMaxCols), min(rows, screen.AppletMaxRows)
	paintPlaceholders(s.id, cols, rows)
	s.send(applet.Message{Type: applet.MsgResize, Cols: cols, Rows: rows})
}

func (s *execSession) send(m applet.Message) {
	if err := applet.WriteMessage(s.conn, m); err != nil {
		fmt.Fprintln(os.Stderr, "tubeless exec:", err)
	}
}

// paintPlaceholders fills the pane with the applet's placeholder cells.
func paintPlaceholders(id, cols, rows int) {
	fg, bg := screen.AppletColors(id)
	var b strings.Builder
	fmt.Fprintf(&b, "\x1b[0m\x1b[38;5;%dm\x1b[48;5;%dm", fg, bg)
	for row := 0; row < rows; row++ {
		fmt.Fprintf(&b, "\x1b[%d;1H", row+1)
		for col := 0; col < cols; col++ {
			b.WriteRune(screen.AppletRune(col, row))
		}
	}
	b.WriteString("\x1b[0m")
	fmt.Fprint(os.Stdout, b.String())
}

// pumpExecStdin reads the pane's input on one goroutine for the whole
// process.
func pumpExecStdin() <-chan []byte {
	out := make(chan []byte, 16)
	go func() {
		defer close(out)
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				out <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				return
			}
		}
	}()
	return out
}
