package main

import (
	"strings"
	"sync/atomic"

	"github.com/go-gl/glfw/v3.4/glfw"

	"github.com/moozd/tubeless/pkg/ptyio"
	"github.com/moozd/tubeless/pkg/render"
	"github.com/moozd/tubeless/pkg/screen"
)

// linesPerNotch is how many scrollback lines one wheel "notch" (yoff of
// ±1, a typical mouse wheel's unit) moves — trackpads report fractional
// yoff for smaller, continuous steps, which this scales the same way.
const linesPerNotch = 3

// scrollState is the render-loop-owned target scroll position (lines back
// from the live tail); Renderer.UpdateScroll eases toward it every frame.
// GLFW callbacks fire during glfw.PollEvents() on the same OS-locked
// thread as the render loop (see main's runtime.LockOSThread), so a plain
// field needs no synchronization despite being written from a callback
// and read from runLoop — unlike the shared *atomic.Pointer[screen.Screen]
// snapshot, which does cross the PTY-coordinator/render-loop goroutine
// boundary.
type scrollState struct {
	target int
}

// mouseState holds everything wireMouse's callbacks need to track between
// events: which button (if any) is currently held for local selection
// drag, and whether the current press is being forwarded to the app as VT
// mouse reporting instead (see Screen.MouseMode) — decided once at press
// time and remembered for the matching release, so a mode change
// mid-drag (unusual, but possible) can't send a press without a matching
// release or vice versa.
type mouseState struct {
	sel       *render.Selection
	dragging  bool
	reporting bool
	lastX     int
	lastY     int
}

// wireMouse wires wheel-scroll, click/drag selection + clipboard copy, and
// VT mouse reporting (SGR 1006 — see pkg/screen's EncodeMouseEvent) for
// apps (vim, tmux, htop) that request it via ?1000/1002/1003h. Whichever
// of local selection/scroll vs. VT reporting applies is decided per-event
// from the latest published Screen's MouseMode: an app that wants mouse
// events gets them instead of the terminal handling clicks/wheel itself.
func wireMouse(win *render.Window, sess *ptyio.Session, shared *atomic.Pointer[screen.Screen], scroll *scrollState, cs *cellSize, sel *render.Selection) {
	ms := &mouseState{sel: sel}

	win.SetScrollCallback(func(_ *glfw.Window, _, yoff float64) {
		scr := shared.Load()
		if scr.MouseMode != screen.MouseOff {
			btn := screen.MouseWheelDown
			if yoff > 0 {
				btn = screen.MouseWheelUp
			}
			shift, alt, ctrl := currentMods(win)
			x, y := cellAt(win, cs)
			sess.Write(screen.EncodeMouseEvent(scr.MouseSGR, btn, screen.MousePress, x, y, shift, alt, ctrl))
			return
		}
		if scr.InAltScreen() {
			// A full-screen app (vim, htop, less) owns the wheel while
			// its own buffer is active — nothing to scroll back through
			// under its own live redraws.
			return
		}
		delta := int(yoff * linesPerNotch)
		scroll.target = clampInt(scroll.target-delta, 0, scr.ScrollbackLen())
	})

	win.SetMouseButtonCallback(func(_ *glfw.Window, button glfw.MouseButton, action glfw.Action, mods glfw.ModifierKey) {
		if button != glfw.MouseButtonLeft && button != glfw.MouseButtonMiddle && button != glfw.MouseButtonRight {
			return
		}
		scr := shared.Load()
		x, y := cellAt(win, cs)

		if action == glfw.Press {
			ms.reporting = scr.MouseMode != screen.MouseOff
			if ms.reporting {
				shift, alt, ctrl := currentMods(win)
				sess.Write(screen.EncodeMouseEvent(scr.MouseSGR, sgrButton(button), screen.MousePress, x, y, shift, alt, ctrl))
				return
			}
			if button != glfw.MouseButtonLeft {
				return
			}
			ms.dragging = true
			*ms.sel = render.Selection{Active: true, StartX: x, StartY: y, EndX: x, EndY: y}
			return
		}

		// action == glfw.Release
		if ms.reporting {
			ms.reporting = false
			shift, alt, ctrl := currentMods(win)
			sess.Write(screen.EncodeMouseEvent(scr.MouseSGR, sgrButton(button), screen.MouseRelease, x, y, shift, alt, ctrl))
			return
		}
		if ms.dragging {
			ms.dragging = false
			copySelectionToClipboard(win, shared, *ms.sel)
			// A plain click (press+release with no movement in between)
			// leaves a zero-width selection that Contains still matches
			// against its one cell — without clearing it here, every
			// click (including the click that just focuses an
			// unfocused window) leaves a permanent single-cell
			// highlight behind, only ever relocated, never removed, by
			// the next click.
			x0, y0, x1, y1 := ms.sel.Normalized()
			if x0 == x1 && y0 == y1 {
				*ms.sel = render.Selection{}
			}
		}
	})

	win.SetCursorPosCallback(func(_ *glfw.Window, xpos, ypos float64) {
		x, y := cellFromPixels(xpos, ypos, cs)
		if x == ms.lastX && y == ms.lastY {
			return
		}
		ms.lastX, ms.lastY = x, y

		scr := shared.Load()
		switch {
		case ms.reporting && scr.MouseMode == screen.MouseAny:
			shift, alt, ctrl := currentMods(win)
			sess.Write(screen.EncodeMouseEvent(scr.MouseSGR, screen.MouseButtonNone, screen.MouseMotion, x, y, shift, alt, ctrl))
		case ms.reporting && scr.MouseMode == screen.MouseDrag:
			shift, alt, ctrl := currentMods(win)
			sess.Write(screen.EncodeMouseEvent(scr.MouseSGR, screen.MouseButtonLeft, screen.MouseMotion, x, y, shift, alt, ctrl))
		case ms.dragging:
			ms.sel.EndX, ms.sel.EndY = x, y
		}
	})
}

// sgrButton maps a GLFW mouse button to the SGR mouse-reporting button
// code (left=0, middle=1, right=2 — X11/xterm's numbering, not GLFW's).
func sgrButton(b glfw.MouseButton) screen.MouseButton {
	switch b {
	case glfw.MouseButtonMiddle:
		return screen.MouseButtonMiddle
	case glfw.MouseButtonRight:
		return screen.MouseButtonRight
	default:
		return screen.MouseButtonLeft
	}
}

func currentMods(win *render.Window) (shift, alt, ctrl bool) {
	press := func(k glfw.Key) bool { return win.GetKey(k) == glfw.Press }
	shift = press(glfw.KeyLeftShift) || press(glfw.KeyRightShift)
	alt = press(glfw.KeyLeftAlt) || press(glfw.KeyRightAlt)
	ctrl = press(glfw.KeyLeftControl) || press(glfw.KeyRightControl)
	return
}

// cellAt is cellFromPixels for the cursor's current position — used by
// callbacks (scroll, button press/release) that don't already have a
// fresh xpos/ypos the way the cursor-position callback does.
func cellAt(win *render.Window, cs *cellSize) (x, y int) {
	xpos, ypos := win.GetCursorPos()
	return cellFromPixels(xpos, ypos, cs)
}

// cellFromPixels converts a GLFW cursor position (screen/logical
// coordinates) to a grid cell, clamped to the visible grid. cs.w/h are
// physical-pixel cell sizes (see cellSize), so the logical position is
// scaled by the content-scale factor first to land in the same space —
// the same relationship pushResizeSize uses going the other direction.
func cellFromPixels(xpos, ypos float64, cs *cellSize) (x, y int) {
	if cs.w <= 0 || cs.h <= 0 {
		return 0, 0
	}
	x = int(float32(xpos) * cs.dpiX / cs.w)
	y = int(float32(ypos) * cs.dpiY / cs.h)
	return max(0, x), max(0, y)
}

// copySelectionToClipboard extracts sel's text from the current grid and
// sets it as the clipboard contents via GLFW (cross-platform: X11/Wayland
// selection on Linux, NSPasteboard on macOS — no xclip/pbcopy shelling
// out needed). A no-op for an empty selection (a click with no drag).
func copySelectionToClipboard(win *render.Window, shared *atomic.Pointer[screen.Screen], sel render.Selection) {
	if !sel.Active {
		return
	}
	scr := shared.Load()
	x0, y0, x1, y1 := sel.Normalized()
	if y0 == y1 && x0 == x1 {
		return
	}
	var b strings.Builder
	for y := y0; y <= y1 && y < scr.Rows; y++ {
		row := scr.Grid[y]
		startX, endX := 0, scr.Cols-1
		if y == y0 {
			startX = x0
		}
		if y == y1 {
			endX = min(x1, scr.Cols-1)
		}
		line := make([]rune, 0, endX-startX+1)
		for x := startX; x <= endX && x < len(row); x++ {
			line = append(line, row[x].Rune)
		}
		b.WriteString(strings.TrimRight(string(line), " "))
		if y < y1 {
			b.WriteByte('\n')
		}
	}
	win.SetClipboardString(b.String())
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
