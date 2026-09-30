// runOpen implements `tubeless open [app_cmd...]`: it runs a GUI app in
// the current tmux pane (or popup). The app itself is hosted by the
// tubeless window (see slots.go); this process is the pane's anchor. It
// fills the pane with slot-marker cells so the window knows where to
// paint the app, and forwards the keys and focus tmux delivers to the
// pane. With no command it shows an app picker first (launcher.go).
package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/moozd/tubeless/pkg/openproto"
	"github.com/moozd/tubeless/pkg/screen"
)

const (
	escapeWait = 25 * time.Millisecond

	// Private modes the pane process turns on while it runs: hide the
	// cursor, report focus, and report the mouse (only so tmux forwards
	// clicks instead of starting a copy-mode selection; the reports are
	// dropped), plus xterm modifyOtherKeys so tmux sends modified keys.
	paneModesOn  = "\x1b[?25l\x1b[?1004h\x1b[?1000h\x1b[?1006h\x1b[>4;2m"
	paneModesOff = "\x1b[>4;0m\x1b[?1006l\x1b[?1000l\x1b[?1004l\x1b[?25h\x1b[0m\x1b[2J\x1b[H"
)

func runOpen(args []string) {
	if os.Getenv("TMUX") == "" {
		fmt.Fprintln(os.Stderr, "tubeless open: only works inside tmux (run it in a pane or a popup)")
		os.Exit(1)
	}
	// One reader for the whole process: a second goroutine left reading
	// stdin would swallow part of every later keystroke.
	stdin := pumpStdin()
	if len(args) == 0 {
		picked, err := pickApp(stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, "tubeless open:", err)
			os.Exit(1)
		}
		if picked == nil {
			os.Exit(0)
		}
		args = picked
	}
	code, err := openInPane(args, stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tubeless open:", err)
	}
	os.Exit(code)
}

func openInPane(argv []string, stdin <-chan []byte) (int, error) {
	conn, err := dialWindow()
	if err != nil {
		return 1, err
	}
	defer conn.Close()
	fd := int(os.Stdin.Fd())
	cols, rows, err := term.GetSize(fd)
	if err != nil {
		return 1, fmt.Errorf("read pane size: %w", err)
	}
	slotID, err := requestSlot(conn, argv, cols, rows)
	if err != nil {
		return 1, err
	}
	saved, err := term.MakeRaw(fd)
	if err != nil {
		return 1, fmt.Errorf("raw mode: %w", err)
	}
	defer restorePane(fd, saved)
	p := &paneClient{conn: conn, slotID: slotID, fd: fd, stdin: stdin}
	return p.run(cols, rows), nil
}

func restorePane(fd int, saved *term.State) {
	fmt.Fprint(os.Stdout, paneModesOff)
	if err := term.Restore(fd, saved); err != nil {
		fmt.Fprintln(os.Stderr, "tubeless open: restore terminal:", err)
	}
}

// requestSlot performs the Open handshake and returns the slot id.
func requestSlot(conn net.Conn, argv []string, cols, rows int) (int, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return 0, fmt.Errorf("working directory: %w", err)
	}
	open := openproto.Open{Argv: argv, Env: os.Environ(), Cwd: cwd, Cols: cols, Rows: rows}
	if err := openproto.WriteMessage(conn, open); err != nil {
		return 0, err
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return 0, fmt.Errorf("set handshake deadline: %w", err)
	}
	msg, err := openproto.ReadMessage(openproto.NewReader(conn))
	if err != nil {
		return 0, fmt.Errorf("read handshake reply: %w", err)
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return 0, fmt.Errorf("clear handshake deadline: %w", err)
	}
	switch m := msg.(type) {
	case openproto.OpenAck:
		return m.SlotID, nil
	case openproto.OpenReject:
		return 0, fmt.Errorf("window refused: %s", m.Reason)
	}
	return 0, fmt.Errorf("unexpected handshake reply %T", msg)
}

// dialWindow finds the tubeless window this pane is shown in. tmux knows
// which terminal each attached client uses, and the window registers
// itself under that tty, so this stays right even when the tmux server
// (and the $TUBELESS_CTL it inherited) came from an older window.
func dialWindow() (net.Conn, error) {
	dir := filepath.Join(runtimeDir(), "tubeless", "tty")
	for _, tty := range paneClientTTYs() {
		if c, err := net.Dial("unix", filepath.Join(dir, ttyLinkName(tty))); err == nil {
			return c, nil
		}
	}
	if ctl := os.Getenv("TUBELESS_CTL"); ctl != "" {
		if c, err := net.Dial("unix", ctl); err == nil {
			return c, nil
		}
	}
	return nil, fmt.Errorf("no tubeless window found for this tmux session (is tmux attached from a tubeless window?)")
}

func runtimeDir() string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return d
	}
	return os.TempDir()
}

// paneClientTTYs lists the ttys of the clients attached to this pane's
// session, most recently active first.
func paneClientTTYs() []string {
	out, err := tmuxOutput(append([]string{"list-clients"}, clientScope()...)...)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tubeless open:", err)
		return nil
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	sort.Sort(sort.Reverse(sort.StringSlice(lines)))
	ttys := make([]string, 0, len(lines))
	for _, l := range lines {
		if _, tty, ok := strings.Cut(l, " "); ok {
			ttys = append(ttys, tty)
		}
	}
	return ttys
}

// clientScope limits list-clients to this pane's session. A popup's
// process has no $TMUX_PANE, so there it lists every client and leans on
// the activity sort: the client that just opened the popup is the most
// recently active one.
func clientScope() []string {
	format := []string{"-F", "#{client_activity} #{client_tty}"}
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		return format
	}
	sid, err := tmuxOutput("display-message", "-p", "-t", pane, "#{session_id}")
	if err != nil {
		fmt.Fprintln(os.Stderr, "tubeless open:", err)
		return format
	}
	return append([]string{"-t", strings.TrimSpace(sid)}, format...)
}

// runPopup implements `tubeless popup [app_cmd...]`: the same as `open`,
// inside a tmux popup centered over the current client.
func runPopup(args []string) {
	if os.Getenv("TMUX") == "" {
		fmt.Fprintln(os.Stderr, "tubeless popup: only works inside tmux")
		os.Exit(1)
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tubeless popup:", err)
		os.Exit(1)
	}
	cmd := []string{"display-popup", "-E", "-w", "80%", "-h", "80%", exe, "open"}
	if err := exec.Command("tmux", append(cmd, args...)...).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "tubeless popup:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func tmuxOutput(args ...string) (string, error) {
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w", args[0], err)
	}
	return string(out), nil
}

// paneClient is the running pane process.
type paneClient struct {
	conn   net.Conn
	slotID int
	fd     int
	stdin  <-chan []byte
}

type windowEvent struct {
	msg any
	err error
}

func (p *paneClient) run(cols, rows int) int {
	p.paint(cols, rows)
	fmt.Fprint(os.Stdout, paneModesOn)
	p.send(openproto.Focus{Focused: true})

	window := p.pumpWindow()
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)

	var dec keyDecoder
	wait := time.NewTimer(time.Hour)
	for {
		select {
		case b, ok := <-p.stdin:
			if !ok {
				return p.close("stdin closed")
			}
			p.forward(dec.Feed(b))
			resetTimer(wait, dec.HasPending())
		case <-wait.C:
			p.forward(dec.Flush())
		case <-winch:
			if c, r, err := term.GetSize(p.fd); err == nil {
				p.send(openproto.Resize{Cols: c, Rows: r})
				p.paint(c, r)
			}
		case ev := <-window:
			if code, done := p.handleWindow(ev); done {
				return code
			}
		case <-quit:
			return p.close("pane closed")
		}
	}
}

func resetTimer(t *time.Timer, pending bool) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	if pending {
		t.Reset(escapeWait)
	}
}

func (p *paneClient) handleWindow(ev windowEvent) (int, bool) {
	if ev.err != nil {
		return 1, true
	}
	switch m := ev.msg.(type) {
	case openproto.Exited:
		return m.Code, true
	case openproto.Close:
		return 0, true
	}
	return 0, false
}

func (p *paneClient) close(reason string) int {
	p.send(openproto.Close{Reason: reason})
	return 0
}

func (p *paneClient) send(msg any) {
	if err := openproto.WriteMessage(p.conn, msg); err != nil {
		fmt.Fprintln(os.Stderr, "tubeless open:", err)
	}
}

func (p *paneClient) forward(events []inputEvent) {
	for _, ev := range events {
		if ev.focus {
			p.send(openproto.Focus{Focused: ev.focused})
			continue
		}
		for _, t := range keyTaps(ev) {
			p.send(openproto.Key{Keysym: t.keysym, Pressed: t.pressed})
		}
	}
}

// paint fills the pane with slot-marker cells in the slot's color.
func (p *paneClient) paint(cols, rows int) {
	var b strings.Builder
	b.WriteString("\x1b[?7l\x1b[0m\x1b[2J")
	marker := strings.Repeat(string(screen.SlotRune), cols)
	for y := 1; y <= rows; y++ {
		fmt.Fprintf(&b, "\x1b[%d;1H\x1b[38;5;%dm%s", y, screen.SlotColorIndex(p.slotID), marker)
	}
	b.WriteString("\x1b[0m\x1b[?7h")
	fmt.Fprint(os.Stdout, b.String())
}

func (p *paneClient) pumpWindow() <-chan windowEvent {
	ch := make(chan windowEvent, 8)
	go func() {
		r := bufio.NewReader(p.conn)
		for {
			msg, err := openproto.ReadMessage(r)
			ch <- windowEvent{msg, err}
			if err != nil {
				return
			}
		}
	}()
	return ch
}

// pumpStdin delivers raw input chunks; the channel closes at EOF.
func pumpStdin() <-chan []byte {
	ch := make(chan []byte, 16)
	go func() {
		defer close(ch)
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				ch <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				return
			}
		}
	}()
	return ch
}
