// runOpen implements `tubeless open <app_cmd...>`: it takes over the
// current tubeless window and renders an arbitrary GUI app inside it.
//
// Since this project controls both ends — the tubeless window process
// (openserver.go) and this launcher — the two talk over a private,
// non-VT protocol (pkg/openproto) rather than PTY escape sequences: the
// window process listens on a per-window Unix control socket exported as
// TUBELESS_CTL (see ptyio.Start), which this process connects out to.
//
// Actually capturing and driving an arbitrary GUI app is delegated to two
// existing, well-tested system tools rather than reimplementing a
// compositor: `cage` (a minimal wlroots kiosk compositor — runs exactly
// one app and exits when it does) and `wayvnc` (an RFB/VNC server for
// wlroots compositors, using wlr-screencopy for capture and
// wlr-virtual-pointer/virtual-keyboard for input injection). cage is
// launched with WLR_BACKENDS=headless — not the default nested-Wayland-
// client backend — so it never creates a host-visible window of its own;
// see the project's gui-app-embedding notes for why that matters even
// when the *window itself* happens to be running under a kiosk cage
// instance (nesting cage-under-cage would otherwise fight that outer
// cage's own "one visible window" policy). wayvnc is pointed at cage's
// nested display over a Unix socket, configured with enable_auth=false
// (generated on the fly — see wayvncConfig) since this connection never
// leaves localhost and both ends are processes this same launch spawns.
package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/moozd/tubeless/pkg/openproto"
	"github.com/moozd/tubeless/pkg/procgroup"
	"github.com/moozd/tubeless/pkg/rfbclient"
)

// openCloseGrace mirrors ptyio's own closeGrace — the same signal-then-
// escalate teardown pattern (see pkg/procgroup), same grace period.
const openCloseGrace = 2 * time.Second

// runOpen is main's entry point for the `open` subcommand. It never
// returns normally — it always os.Exit's with the outcome.
func runOpen(appCmd []string) {
	if len(appCmd) == 0 {
		fmt.Fprintln(os.Stderr, "usage: tubeless open <app_cmd> [args...]")
		os.Exit(2)
	}
	ctlPath := os.Getenv("TUBELESS_CTL")
	if ctlPath == "" {
		fmt.Fprintln(os.Stderr, "tubeless open: TUBELESS_CTL is not set — this only works inside a real tubeless window (not nested in another terminal, and not over SSH)")
		os.Exit(1)
	}
	if err := checkOpenDeps(); err != nil {
		fmt.Fprintln(os.Stderr, "tubeless open:", err)
		os.Exit(1)
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		fmt.Fprintln(os.Stderr, "tubeless open: XDG_RUNTIME_DIR is not set — needed to find cage's own nested Wayland display")
		os.Exit(1)
	}

	o := &openRunner{}
	os.Exit(o.run(ctlPath, runtimeDir, appCmd))
}

// checkOpenDeps reports a clear, actionable error naming exactly what's
// missing rather than letting exec.Command fail deep inside run() with a
// bare "executable file not found in $PATH".
func checkOpenDeps() error {
	var missing []string
	for _, bin := range []string{"cage", "wayvnc"} {
		if _, err := exec.LookPath(bin); err != nil {
			missing = append(missing, bin)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("needs %s on PATH — install them (e.g. `sudo pacman -S %s` on Arch, `sudo apt install %s` on Debian/Ubuntu)",
		strings.Join(missing, " and "), strings.Join(missing, " "), strings.Join(missing, " "))
}

// openRunner holds every piece runOpen's teardown needs to reach from
// whichever goroutine notices the session should end first (the app
// exiting normally, the control connection dying, or this process being
// signaled) — see shutdown.
type openRunner struct {
	ctlConn net.Conn
	rfb     *rfbclient.Client

	cage, wayvnc *procgroup.Watched

	shutdownOnce sync.Once
	exitCode     int
}

func (o *openRunner) run(ctlPath, runtimeDir string, appCmd []string) int {
	workDir, err := os.MkdirTemp("", "tubeless-open-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "tubeless open: create work dir:", err)
		return 1
	}
	defer os.RemoveAll(workDir)

	before, err := waylandSocketSet(runtimeDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tubeless open: list", runtimeDir, ":", err)
		return 1
	}

	cageCmd := buildCageCmd(appCmd)
	if err := cageCmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "tubeless open: start cage:", err)
		return 1
	}
	o.cage = procgroup.Watch(cageCmd)

	nestedDisplay, err := waitForNewWaylandSocket(runtimeDir, before, 5*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tubeless open:", err)
		o.killCage()
		return 1
	}

	vncSockPath := filepath.Join(workDir, "wayvnc.sock")
	confPath := filepath.Join(workDir, "wayvnc.conf")
	if err := os.WriteFile(confPath, []byte("enable_auth=false\n"), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "tubeless open: write wayvnc config:", err)
		o.killCage()
		return 1
	}

	wayvncCmd := buildWayvncCmd(nestedDisplay, confPath, vncSockPath)
	if err := wayvncCmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "tubeless open: start wayvnc:", err)
		o.killCage()
		return 1
	}
	o.wayvnc = procgroup.Watch(wayvncCmd)

	rfb, err := dialRFBWithRetry(vncSockPath, 3*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tubeless open: connect to wayvnc:", err)
		o.killWayvnc()
		o.killCage()
		return 1
	}
	o.rfb = rfb

	ctlConn, err := net.Dial("unix", ctlPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tubeless open: connect to window:", err)
		o.rfb.Close()
		o.killWayvnc()
		o.killCage()
		return 1
	}
	o.ctlConn = ctlConn
	if err := o.handshake(appCmd); err != nil {
		fmt.Fprintln(os.Stderr, "tubeless open:", err)
		o.shutdown("handshake failed", 1)
		return o.exitCode
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	go o.pumpRFBToWindow()
	go o.pumpWindowToRFB()

	// cage.Done/wayvnc.Done are closed channels (see procgroup.Watched) —
	// safe to read here AND again later from shutdown's own
	// killCage/killWayvnc calls, unlike a single-value buffered channel
	// where only one of the two readers would ever get the value (the
	// bug an earlier version of this file actually hit: this select and
	// shutdown's own teardown each tried to consume the same channel,
	// and whichever lost the race hung forever).
	select {
	case <-o.cage.Done:
		o.shutdown("app exited", 0)
	case <-o.wayvnc.Done:
		o.shutdown("wayvnc exited unexpectedly", 1)
	case sig := <-sigCh:
		o.shutdown(fmt.Sprintf("received %s", sig), 0)
	}
	return o.exitCode
}

// handshake sends OpenRequest and waits (bounded — a hung window process
// must not leave this launcher stuck forever) for OpenAck/OpenReject.
func (o *openRunner) handshake(appCmd []string) error {
	title := filepath.Base(appCmd[0])
	if err := openproto.WriteMessage(o.ctlConn, openproto.OpenRequest{Title: title}); err != nil {
		return fmt.Errorf("send OpenRequest: %w", err)
	}
	o.ctlConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	msg, err := openproto.ReadMessage(openproto.NewReader(o.ctlConn))
	o.ctlConn.SetReadDeadline(time.Time{})
	if err != nil {
		return fmt.Errorf("read handshake reply: %w", err)
	}
	switch m := msg.(type) {
	case openproto.OpenAck:
		return nil
	case openproto.OpenReject:
		return fmt.Errorf("window rejected the session: %s", m.Reason)
	default:
		return fmt.Errorf("unexpected handshake reply %T", msg)
	}
}

// pumpRFBToWindow is the RFB read side: request a full update once (so
// the very first frame isn't blank), then loop requesting incremental
// updates and forwarding whatever comes back — pixel patches as
// FrameRect, resize notifications as Resize — to the window process.
func (o *openRunner) pumpRFBToWindow() {
	if err := o.rfb.RequestUpdate(false); err != nil {
		o.shutdown(fmt.Sprintf("request initial update: %v", err), 1)
		return
	}
	for {
		updates, err := o.rfb.ReadUpdate()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.EOF) {
				log.Printf("tubeless open: read RFB update: %v", err)
			}
			o.shutdown("wayvnc connection closed", 1)
			return
		}
		for _, u := range updates {
			var msg any
			if u.Resized {
				msg = openproto.Resize{W: u.W, H: u.H}
			} else {
				msg = openproto.FrameRect{X: u.X, Y: u.Y, W: u.W, H: u.H, Pix: u.Pix}
			}
			if err := openproto.WriteMessage(o.ctlConn, msg); err != nil {
				o.shutdown(fmt.Sprintf("send to window: %v", err), 1)
				return
			}
		}
		if err := o.rfb.RequestUpdate(true); err != nil {
			o.shutdown(fmt.Sprintf("request update: %v", err), 1)
			return
		}
	}
}

// pumpWindowToRFB is the control-socket read side: translates the
// window's InputEvent/Resize messages into RFB PointerEvent/KeyEvent/
// SetDesktopSize calls against wayvnc.
func (o *openRunner) pumpWindowToRFB() {
	ptr := &pointerState{}
	r := openproto.NewReader(o.ctlConn)
	for {
		msg, err := openproto.ReadMessage(r)
		if err != nil {
			if !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.EOF) {
				log.Printf("tubeless open: read from window: %v", err)
			}
			o.shutdown("window connection closed", 0)
			return
		}
		switch m := msg.(type) {
		case openproto.InputEvent:
			if err := applyInputEvent(o.rfb, ptr, m); err != nil {
				log.Printf("tubeless open: forward input: %v", err)
			}
		case openproto.Resize:
			// Best-effort — see pkg/rfbclient.SetDesktopSize's own doc
			// comment on how unverified this path still is against real
			// wayvnc resize behavior.
			if err := o.rfb.SetDesktopSize(m.W, m.H); err != nil {
				log.Printf("tubeless open: request resize to %dx%d: %v", m.W, m.H, err)
			}
		case openproto.Close:
			o.shutdown("window requested close: "+m.Reason, 0)
			return
		default:
			log.Printf("tubeless open: unexpected message %T from window", msg)
		}
	}
}

// pointerState is the cumulative button mask RFB's PointerEvent requires
// on every single event (RFB has no separate press/release message — see
// pkg/rfbclient's own doc comment on ButtonLeft etc).
type pointerState struct {
	mask uint8
}

func (p *pointerState) setButton(btn openproto.PointerButton, pressed bool) {
	var bit uint8
	switch btn {
	case openproto.PointerLeft:
		bit = rfbclient.ButtonLeft
	case openproto.PointerMiddle:
		bit = rfbclient.ButtonMiddle
	case openproto.PointerRight:
		bit = rfbclient.ButtonRight
	}
	if pressed {
		p.mask |= bit
	} else {
		p.mask &^= bit
	}
}

// applyInputEvent translates one window-side InputEvent into the RFB
// call(s) it corresponds to.
func applyInputEvent(c *rfbclient.Client, ptr *pointerState, ev openproto.InputEvent) error {
	switch ev.Kind {
	case openproto.InputPointerMove:
		return c.SendPointerEvent(ptr.mask, ev.X, ev.Y)
	case openproto.InputPointerButton:
		ptr.setButton(ev.Button, ev.Pressed)
		return c.SendPointerEvent(ptr.mask, ev.X, ev.Y)
	case openproto.InputPointerScroll:
		return sendScroll(c, ptr, ev.X, ev.Y, ev.Scroll)
	case openproto.InputKey:
		return c.SendKeyEvent(keysymFor(ev), ev.Pressed)
	default:
		return fmt.Errorf("unknown InputEvent kind %d", ev.Kind)
	}
}

// sendScroll models a wheel notch as RFB conventionally does: a quick
// press+release pulse of the wheel-up/wheel-down button bit at the
// pointer's current position, on top of whatever real buttons are
// already held (ptr.mask).
func sendScroll(c *rfbclient.Client, ptr *pointerState, x, y, notches int) error {
	bit := uint8(rfbclient.ButtonWheelUp)
	if notches < 0 {
		bit = rfbclient.ButtonWheelDn
		notches = -notches
	}
	for i := 0; i < notches; i++ {
		if err := c.SendPointerEvent(ptr.mask|bit, x, y); err != nil {
			return err
		}
		if err := c.SendPointerEvent(ptr.mask, x, y); err != nil {
			return err
		}
	}
	return nil
}

// keysymFor resolves an InputEvent to the X11 keysym RFB's KeyEvent
// wants: Code already *is* a keysym value (see pkg/openproto.KeyCode's
// own doc comment) for a non-printable/modifier key; a printable
// character maps via the standard X11 rule — Latin-1 keysyms equal their
// code point (0x20-0xff) directly, anything past that is the Unicode
// extension range (0x01000000 + code point).
func keysymFor(ev openproto.InputEvent) uint32 {
	if ev.Code != 0 {
		return uint32(ev.Code)
	}
	r := ev.Rune
	if r >= 0x20 && r <= 0xff {
		return uint32(r)
	}
	return 0x01000000 + uint32(r)
}

// buildCageCmd launches `cage -- appCmd...` with the headless wlroots
// backend (see this file's own package doc comment on why) — no
// WAYLAND_DISPLAY/DISPLAY inherited, so it can't accidentally nest as a
// client of whatever compositor this process happens to be running
// under.
func buildCageCmd(appCmd []string) *exec.Cmd {
	args := append([]string{"--"}, appCmd...)
	cmd := exec.Command("cage", args...)
	cmd.Env = filterEnv(os.Environ(), "WAYLAND_DISPLAY", "DISPLAY")
	cmd.Env = append(cmd.Env, "WLR_BACKENDS=headless", "WLR_LIBINPUT_NO_DEVICES=1")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	procgroup.Setup(cmd)
	return cmd
}

// buildWayvncCmd points wayvnc at cage's own nested display and has it
// listen on a Unix socket only — never a TCP port, so this never becomes
// reachable from anywhere but this launcher. -r enables overlay cursor
// rendering (otherwise the embedded app's own cursor position is
// invisible in the captured frames — nothing paints a hardware cursor
// without it); -e exits wayvnc the moment this, its only ever client,
// disconnects, which also lets pumpRFBToWindow's own error path double
// as the detector for "wayvnc went away" via wayvncDone.
func buildWayvncCmd(nestedDisplay, confPath, sockPath string) *exec.Cmd {
	cmd := exec.Command("wayvnc", "-r", "-e", "-C", confPath, "unix:"+sockPath)
	cmd.Env = append(os.Environ(), "WAYLAND_DISPLAY="+nestedDisplay)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	procgroup.Setup(cmd)
	return cmd
}

// filterEnv returns env with every entry whose key is in drop removed —
// used to strip WAYLAND_DISPLAY/DISPLAY before launching cage headless.
func filterEnv(env []string, drop ...string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		key, _, _ := strings.Cut(e, "=")
		keep := true
		for _, d := range drop {
			if key == d {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, e)
		}
	}
	return out
}

// waylandSocketSet lists the Wayland server sockets currently present in
// dir (XDG_RUNTIME_DIR) — the "before" snapshot waitForNewWaylandSocket
// diffs cage's own nested display out of, since cage (run with no -D)
// prints nothing identifying which display name it picked.
func waylandSocketSet(dir string) (map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(entries))
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "wayland-") && !strings.HasSuffix(name, ".lock") {
			set[name] = true
		}
	}
	return set, nil
}

// waitForNewWaylandSocket polls dir until a Wayland socket not present in
// before shows up (cage creating its own nested display) or timeout
// elapses.
func waitForNewWaylandSocket(dir string, before map[string]bool, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		after, err := waylandSocketSet(dir)
		if err == nil {
			for name := range after {
				if !before[name] {
					return name, nil
				}
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("timed out waiting for cage's nested Wayland display to appear in %s", dir)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// dialRFBWithRetry retries the initial connect briefly — wayvnc needs a
// moment after Start() to create and bind its socket, the same kind of
// startup race probeMaxTextureSizeWithRetry works around elsewhere in
// this codebase.
func dialRFBWithRetry(sockPath string, timeout time.Duration) (*rfbclient.Client, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		c, err := rfbclient.Dial("unix", sockPath)
		if err == nil {
			return c, nil
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	return nil, lastErr
}

func (o *openRunner) killCage() {
	if o.cage != nil {
		o.cage.Close(openCloseGrace)
	}
}

func (o *openRunner) killWayvnc() {
	if o.wayvnc != nil {
		o.wayvnc.Close(openCloseGrace)
	}
}

// shutdown is the single teardown path, safe to call concurrently from
// any of pumpRFBToWindow/pumpWindowToRFB/run's own select — whichever
// caller wins the race actually runs it; every other caller's own
// exitCode/reason is dropped, matching "the first thing that noticed the
// session should end decides why" rather than trying to merge reasons.
func (o *openRunner) shutdown(reason string, exitCode int) {
	o.shutdownOnce.Do(func() {
		log.Printf("tubeless open: shutting down: %s", reason)
		o.exitCode = exitCode
		if o.ctlConn != nil {
			openproto.WriteMessage(o.ctlConn, openproto.Close{Reason: reason})
			o.ctlConn.Close()
		}
		if o.rfb != nil {
			o.rfb.Close()
		}
		o.killWayvnc()
		o.killCage()
	})
}
