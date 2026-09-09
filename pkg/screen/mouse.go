package screen

import "fmt"

// MouseButton identifies which button a mouse event is about, in the
// numbering SGR mouse reporting (CSI < ... M/m) uses on the wire.
type MouseButton int

const (
	MouseButtonLeft   MouseButton = 0
	MouseButtonMiddle MouseButton = 1
	MouseButtonRight  MouseButton = 2
	MouseButtonNone   MouseButton = 3 // motion with no button held (?1003)
	MouseWheelUp      MouseButton = 64
	MouseWheelDown    MouseButton = 65
)

// MouseEventKind is what happened — press, release, or (drag/any-motion
// reporting) plain movement.
type MouseEventKind int

const (
	MousePress MouseEventKind = iota
	MouseRelease
	MouseMotion
)

// EncodeMouseEvent builds an SGR mouse-reporting sequence (CSI < ... M for
// press/motion, CSI < ... m for release) — the modern, unambiguous mouse
// protocol (DEC private mode 1006), which is the only encoding this
// package implements: the legacy X10/normal encoding (mode 1000 without
// 1006) crams coordinates into single bytes and breaks past column/row
// 223, which is trivially exceeded by any real terminal size, so there's
// no reason to emit it even though ?1000/1002/1003 alone (without ?1006)
// is technically asking for that legacy form — an app enabling mouse
// reporting virtually always enables ?1006 alongside it for exactly this
// reason. col/row are 0-based (matching Screen.CursorX/Y elsewhere in
// this package); the wire protocol is 1-based.
func EncodeMouseEvent(btn MouseButton, kind MouseEventKind, col, row int, shift, alt, ctrl bool) []byte {
	code := int(btn)
	if kind == MouseMotion {
		code |= 32
	}
	if shift {
		code |= 4
	}
	if alt {
		code |= 8
	}
	if ctrl {
		code |= 16
	}
	final := byte('M')
	if kind == MouseRelease {
		final = 'm'
	}
	return fmt.Appendf(nil, "\x1b[<%d;%d;%d%c", code, col+1, row+1, final)
}
