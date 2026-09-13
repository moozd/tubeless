package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/go-gl/glfw/v3.4/glfw"
)

// xclipTimeout bounds how long a read waits on the X11 selection owner
// before giving up and falling back to the Wayland clipboard.
const xclipTimeout = 250 * time.Millisecond

// clipboardSink is the GLFW clipboard access the X11 bridge supplements or
// falls back to. *render.Window satisfies it via its embedded *glfw.Window.
type clipboardSink interface {
	GetClipboardString() string
	SetClipboardString(string)
}

// bridgeX11 reports whether this run should reach the X11 CLIPBOARD too:
// the Wayland build (whose GLFW clipboard doesn't see the X11 selection
// that xclip/xsel write) with XWayland present (DISPLAY set). Pure
// Wayland, the X11 build (GLFW already reads/writes X11 directly), and
// macOS all skip it.
var bridgeX11 = func() bool {
	return glfw.GetPlatform() == glfw.PlatformWayland && os.Getenv("DISPLAY") != ""
}

// xclipRead reads the X11 CLIPBOARD via xclip, returning the content
// verbatim and whether the read succeeded.
var xclipRead = func() (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), xclipTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "xclip", "-o", "-selection", "clipboard").Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

// xclipWrite sets the X11 CLIPBOARD via xclip. Start (not Run) lets xclip
// keep serving the selection in the background; the goroutine Wait reaps
// it once another owner takes the selection.
var xclipWrite = func(text string) {
	cmd := exec.Command("xclip", "-in", "-selection", "clipboard")
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Start(); err == nil {
		go cmd.Wait()
	}
}

// readClipboard returns the clipboard text, preferring the X11 CLIPBOARD
// (where tmux's copy-pipe with xclip lands) when bridging, and falling
// back to the Wayland clipboard otherwise.
func readClipboard(win clipboardSink) string {
	if bridgeX11() {
		if text, ok := xclipRead(); ok && text != "" {
			return text
		}
	}
	return win.GetClipboardString()
}

// writeClipboard sets the clipboard on both Wayland (GLFW) and X11
// (xclip) so the two stay in sync for the hybrid XWayland workflow.
func writeClipboard(win clipboardSink, text string) {
	if bridgeX11() {
		xclipWrite(text)
	}
	win.SetClipboardString(text)
}
