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

// EncodeMouseEvent builds a mouse-reporting sequence for whichever
// encoding the app actually negotiated: SGR (CSI < ... M/m, DEC private
// mode 1006) when sgr is true, legacy X10/normal (CSI M Cb Cx Cy, mode
// 1000/1002/1003 without 1006) otherwise. Sending SGR unconditionally
// used to be this package's whole approach on the assumption that any app
// enabling mouse reporting also enables ?1006 — not reliably true (tmux
// depends on what it detects from the outer TERM/terminfo) — so an app
// that only asked for the legacy form couldn't parse the bytes it got,
// and those undecodable bytes ended up echoed back as visible garbage
// text a user could go on to select and copy. col/row are 0-based
// (matching Screen.CursorX/Y elsewhere in this package); the wire
// protocol is 1-based.
func EncodeMouseEvent(sgr bool, btn MouseButton, kind MouseEventKind, col, row int, shift, alt, ctrl bool) []byte {
	if !sgr {
		return encodeLegacyMouseEvent(btn, kind, col, row, shift, alt, ctrl)
	}
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

// encodeLegacyMouseEvent builds a legacy X10/normal mouse-reporting
// sequence (ESC [ M Cb Cx Cy): each field is one byte offset by 32 so
// it's always printable, which caps coordinates at 223 (255-32)
// columns/rows — clamped here rather than wrapping into control-character
// byte values the app would misinterpret. Release can't identify which
// button let go (the format has no field for it), so it always reports
// code 3, per the legacy spec.
func encodeLegacyMouseEvent(btn MouseButton, kind MouseEventKind, col, row int, shift, alt, ctrl bool) []byte {
	code := int(btn)
	if kind == MouseRelease {
		code = 3
	}
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
	clamp := func(v int) byte {
		if v > 223 {
			v = 223
		}
		return byte(32 + v)
	}
	return []byte{0x1b, '[', 'M', byte(32 + code), clamp(col + 1), clamp(row + 1)}
}
