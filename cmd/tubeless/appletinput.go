package main

import (
	"context"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/go-gl/glfw/v3.4/glfw"

	"github.com/moozd/tubeless/pkg/applet/cef"
	"github.com/moozd/tubeless/pkg/config"
	"github.com/moozd/tubeless/pkg/render"
	"github.com/moozd/tubeless/pkg/screen"
)

// wheelNotch is how many pixels one wheel notch scrolls a page, the
// same as Chromium's own WHEEL_DELTA.
const wheelNotch = 120

// doubleClickTime bounds the gap between clicks that count as one
// double or triple click.
const doubleClickTime = 400 * time.Millisecond

// appletPoint is a pointer position on an applet, in page DIPs.
type appletPoint struct {
	applet *openApplet
	x, y   int
}

// appletInput is the pointer and keyboard state the host keeps between
// GLFW callbacks. Every method runs on the main thread.
type appletInput struct {
	hovered       *openApplet
	captured      *openApplet
	lastPoint     appletPoint
	clickCount    int
	lastClick     time.Time
	isWinFocused  bool
	prefix        keyChord
	prefixPending bool
	scanOffset    int
	// charToTerminal sends the char event of the key after the prefix
	// to the terminal too: GLFW reports it after the key event.
	charToTerminal bool
}

// keyChord is one key with its modifiers, such as tmux's C-a.
type keyChord struct {
	key  glfw.Key
	mods glfw.ModifierKey
}

const chordMods = glfw.ModShift | glfw.ModControl | glfw.ModAlt | glfw.ModSuper

func (c keyChord) matches(key glfw.Key, mods glfw.ModifierKey) bool {
	return c.key != glfw.KeyUnknown && c.key == key && c.mods == mods&chordMods
}

// readTmuxPrefix asks tmux for its prefix key, so the prefix still
// reaches tmux while an applet has the keyboard.
func readTmuxPrefix() keyChord {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "show", "-gv", "prefix").Output()
	if err != nil {
		log.Printf("applets: no tmux prefix (%v); applet keys never go to tmux", err)
		return keyChord{key: glfw.KeyUnknown}
	}
	return parseTmuxKey(strings.TrimSpace(string(out)))
}

// parseTmuxKey reads a tmux key name such as "C-a", "M-Space" or "`".
func parseTmuxKey(name string) keyChord {
	c := keyChord{key: glfw.KeyUnknown}
	for len(name) > 2 && name[1] == '-' {
		switch name[0] {
		case 'C':
			c.mods |= glfw.ModControl
		case 'M':
			c.mods |= glfw.ModAlt
		case 'S':
			c.mods |= glfw.ModShift
		}
		name = name[2:]
	}
	c.key = glfwKeyByName(name)
	return c
}

func glfwKeyByName(name string) glfw.Key {
	if name == "Space" {
		return glfw.KeySpace
	}
	if len(name) != 1 {
		return glfw.KeyUnknown
	}
	r := rune(strings.ToLower(name)[0])
	for k := glfw.KeySpace; k <= glfw.KeyGraveAccent; k++ {
		if b, ok := baseRune(k); ok && b == r {
			return k
		}
	}
	return glfw.KeyUnknown
}

// pointAt finds the applet under a framebuffer pixel, if any.
func (h *appletHost) pointAt(px, py float32, fbW, fbH int, cs *cellSize, ar config.AspectRatio, scr *screen.Screen) (appletPoint, bool) {
	if cs.w <= 0 || cs.h <= 0 {
		return appletPoint{}, false
	}
	bx, by, bw, bh := render.LetterboxBox(ar, fbW, fbH)
	offX, offY := render.GridOffset(float32(bw), float32(bh), scr.Cols, scr.Rows, cs.w, cs.h)
	gx, gy := px-float32(bx)-offX, py-float32(by)-offY
	cx, cy := int(gx/cs.w), int(gy/cs.h)
	if gx < 0 || gy < 0 || cy >= len(scr.Grid) || cx >= len(scr.Grid[cy]) {
		return appletPoint{}, false
	}
	id, col, row, ok := scr.Grid[cy][cx].AppletCell()
	a, isOpen := h.applets[id]
	if !ok || !isOpen {
		return appletPoint{}, false
	}
	lx := float32(col)*cs.w + gx - float32(cx)*cs.w
	ly := float32(row)*cs.h + gy - float32(cy)*cs.h
	return appletPoint{applet: a, x: int(lx / a.scale), y: int(ly / a.scale)}, true
}

// PointerMove follows the pointer. It reports whether an applet took
// the event; a held button keeps sending to the applet it pressed on.
func (h *appletHost) PointerMove(pt appletPoint, isOver bool, mods uint32) bool {
	if h == nil {
		return false
	}
	in := &h.input
	if in.captured != nil {
		target := in.lastPoint
		if isOver && pt.applet == in.captured {
			target = pt
		}
		in.lastPoint = target
		in.captured.browser.MouseMove(target.x, target.y, mods, false)
		return true
	}
	if in.hovered != nil && (!isOver || pt.applet != in.hovered) {
		in.hovered.browser.MouseMove(in.lastPoint.x, in.lastPoint.y, mods, true)
		in.hovered = nil
	}
	if !isOver {
		return false
	}
	in.hovered, in.lastPoint = pt.applet, pt
	pt.applet.browser.MouseMove(pt.x, pt.y, mods, false)
	return true
}

// PointerButton presses or releases a button. It reports whether an
// applet took the event.
func (h *appletHost) PointerButton(pt appletPoint, isOver bool, button cef.MouseButton, isUp bool, mods uint32) bool {
	if h == nil {
		return false
	}
	in := &h.input
	if isUp && in.captured != nil {
		in.captured.browser.MouseClick(in.lastPoint.x, in.lastPoint.y, mods, button, true, in.clickCount)
		in.captured = nil
		return true
	}
	if !isOver || isUp {
		return false
	}
	in.countClick(pt)
	in.captured, in.lastPoint = pt.applet, pt
	pt.applet.browser.MouseClick(pt.x, pt.y, mods, button, false, in.clickCount)
	return true
}

func (in *appletInput) countClick(pt appletPoint) {
	now := time.Now()
	isRepeat := now.Sub(in.lastClick) < doubleClickTime && pt.applet == in.lastPoint.applet &&
		abs(pt.x-in.lastPoint.x) < 4 && abs(pt.y-in.lastPoint.y) < 4
	in.lastClick = now
	if !isRepeat || in.clickCount >= 3 {
		in.clickCount = 1
		return
	}
	in.clickCount++
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// PointerWheel scrolls the applet under the pointer. It reports whether
// an applet took the event.
func (h *appletHost) PointerWheel(pt appletPoint, isOver bool, xoff, yoff float64, mods uint32) bool {
	if h == nil || !isOver {
		return false
	}
	pt.applet.browser.MouseWheel(pt.x, pt.y, mods, int(xoff*wheelNotch), int(yoff*wheelNotch))
	return true
}

// SetWindowFocused follows the tubeless window's own focus.
func (h *appletHost) SetWindowFocused(isFocused bool) {
	if h == nil {
		return
	}
	h.input.isWinFocused = isFocused
	h.syncFocus()
}

// syncFocus gives keyboard focus to the applet whose pane tmux reports
// as focused, while the window itself has focus.
func (h *appletHost) syncFocus() {
	for _, a := range h.applets {
		has := h.input.isWinFocused && a.isPaneFocused
		if has != a.hasFocus {
			a.hasFocus = has
			a.browser.Focus(has)
			log.Printf("applets: applet %d keyboard focus %v", a.id, has)
		}
	}
}

func (h *appletHost) keyTarget() *openApplet {
	for _, a := range h.applets {
		if a.hasFocus && !a.isClosing {
			return a
		}
	}
	return nil
}

// Key sends a key to the focused applet. It reports false when the key
// belongs to the terminal instead: no applet has focus, or it is the
// tmux prefix or the key right after it.
func (h *appletHost) Key(key glfw.Key, scancode int, action glfw.Action, mods glfw.ModifierKey) bool {
	if h == nil {
		return false
	}
	a := h.keyTarget()
	if a == nil {
		return false
	}
	h.input.charToTerminal = false
	if action == glfw.Press && h.takePrefix(key, mods) {
		return false
	}
	if cmd, isEdit := editCommand(key, mods); isEdit {
		runEdit(a, cmd, action)
		return true
	}
	events := keyEvents(key, scancode+h.input.scanOffset, action, mods)
	for _, ev := range withNativeChars(events) {
		a.browser.Key(ev)
	}
	return true
}

// runEdit runs an edit shortcut on press and repeat. Its release goes
// nowhere: the page never saw the key go down.
func runEdit(a *openApplet, cmd cef.EditCommand, action glfw.Action) {
	if action == glfw.Release {
		return
	}
	a.browser.Edit(cmd)
}

// takePrefix reports whether this key press goes to tmux: the prefix
// itself, or the one key after it.
func (h *appletHost) takePrefix(key glfw.Key, mods glfw.ModifierKey) bool {
	in := &h.input
	if in.prefixPending {
		if !isModifierKey(key) {
			in.prefixPending, in.charToTerminal = false, true
		}
		return true
	}
	if in.prefix.matches(key, mods) {
		in.prefixPending = true
		return true
	}
	return false
}

func isModifierKey(key glfw.Key) bool {
	return key >= glfw.KeyLeftShift && key <= glfw.KeyRightSuper
}

// Char sends typed text to the focused applet. It reports false when
// the text belongs to the terminal.
func (h *appletHost) Char(r rune, mods glfw.ModifierKey) bool {
	if h == nil {
		return false
	}
	if h.input.charToTerminal {
		h.input.charToTerminal = false
		return false
	}
	a := h.keyTarget()
	if a == nil || h.input.prefixPending {
		return false
	}
	for _, ev := range charEvents(r, mods) {
		a.browser.Key(ev)
	}
	return true
}

// forget drops every pointer reference to a closed applet.
func (in *appletInput) forget(a *openApplet) {
	if in.hovered == a {
		in.hovered = nil
	}
	if in.captured == a {
		in.captured = nil
	}
	if in.lastPoint.applet == a {
		in.lastPoint = appletPoint{}
	}
}
