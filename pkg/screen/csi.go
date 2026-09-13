package screen

import "fmt"

// Kitty keyboard protocol flag bits
// (https://sw.kovidgoyal.net/kitty/keyboard-protocol/). Exported so
// cmd/tubeless's input handling can check which enhancements an app has
// enabled before encoding a key event.
const (
	KittyDisambiguate     = 1 << iota // report ambiguous keys (Esc, ctrl/alt combos) as CSI u
	KittyReportEvents                 // report key repeat and release events
	KittyReportAlternate              // report shifted + base-layout keys for shortcut matching
	KittyReportAllKeys                // report all keys (including text) as escape codes
	KittyReportAssociated             // report the text a key produces
)

// KittySupportedFlags is the bitmask advertised in response to the kitty
// protocol's "CSI ? u" query — everything above is implemented.
const KittySupportedFlags = KittyDisambiguate | KittyReportEvents | KittyReportAlternate | KittyReportAllKeys | KittyReportAssociated

// kittyMaxStack bounds the per-screen push/pop stack so a hostile app can't
// grow it without bound (the spec requires a limit).
const kittyMaxStack = 64

func (h *Handler) CSIDispatch(final byte, params []int, subs [][]int, intermediates []byte, private byte) {
	switch final {
	case 'A':
		h.MoveBy(0, -param(params, 0, 1))
	case 'B':
		h.MoveBy(0, param(params, 0, 1))
	case 'C':
		h.MoveBy(param(params, 0, 1), 0)
	case 'D':
		h.MoveBy(-param(params, 0, 1), 0)
	case 'H', 'f':
		h.MoveTo(param(params, 1, 1)-1, param(params, 0, 1)-1)
	case 'G', '`':
		h.MoveTo(param(params, 0, 1)-1, h.CursorY)
	case 'd':
		h.MoveTo(h.CursorX, param(params, 0, 1)-1)
	case 'J':
		h.EraseInDisplay(EraseMode(param(params, 0, 0)))
	case 'K':
		h.EraseInLine(EraseMode(param(params, 0, 0)))
	case 'L':
		h.InsertLines(param(params, 0, 1))
	case 'M':
		h.DeleteLines(param(params, 0, 1))
	case '@':
		h.InsertChars(param(params, 0, 1))
	case 'P':
		h.DeleteChars(param(params, 0, 1))
	case 'X':
		h.EraseChars(param(params, 0, 1))
	case 'S':
		h.ScrollUp(param(params, 0, 1))
	case 'T':
		h.ScrollDown(param(params, 0, 1))
	case 's':
		h.saveCursor()
	case 'u':
		// A private marker (or the kitty protocol's '=' marker, also in
		// the private range) distinguishes the kitty keyboard sequences
		// (CSI ?/>/</= ... u) from ECMA-48 RC (bare "CSI u").
		if private == 0 {
			h.restoreCursor()
			return
		}
		h.dispatchKittyKeyboard(params, private)
	case 'm':
		// A private marker means this isn't SGR at all, just a sequence
		// that happens to also end in 'm' — e.g. xterm's "CSI > 4;2 m"
		// modifyOtherKeys negotiation. Applying it as SGR would misread
		// its params as color/attribute codes.
		if private == '>' {
			h.setModifyOtherKeys(params)
			return
		}
		if private == 0 {
			h.applySGR(params, subs)
		}
	case 'r':
		h.SetScrollRegion(param(params, 0, 1)-1, param(params, 1, h.Rows)-1)
	case 'h':
		h.setMode(private, params, true)
	case 'l':
		h.setMode(private, params, false)
	}
}

// setMouseMode applies a mouse-reporting private mode (?1000/1002/1003):
// disabling any of the three turns mouse reporting off entirely (there's
// only one active mode at a time, matching real terminals — enabling
// ?1002 after ?1000 replaces it rather than stacking), not just the one
// named mode.
func (h *Handler) setMouseMode(mode MouseMode, enabled bool) {
	if enabled {
		h.MouseMode = mode
	} else if h.MouseMode == mode {
		h.MouseMode = MouseOff
	}
}

func param(params []int, idx, def int) int {
	if idx >= len(params) || params[idx] == 0 {
		return def
	}
	return params[idx]
}

func (h *Handler) setMode(private byte, params []int, enabled bool) {
	if private != '?' {
		return
	}
	for _, p := range params {
		switch p {
		case 1:
			h.ApplicationCursorKeys = enabled
		case 6:
			h.OriginMode = enabled
		case 7:
			h.AutoWrap = enabled
		case 25:
			h.CursorVisible = enabled
		case 47, 1047, 1049:
			if enabled {
				h.EnterAltScreen()
			} else {
				h.ExitAltScreen()
			}
		case 1000:
			h.setMouseMode(MouseClick, enabled)
		case 1002:
			h.setMouseMode(MouseDrag, enabled)
		case 1003:
			h.setMouseMode(MouseAny, enabled)
		case 1006:
			h.MouseSGR = enabled
		case 2004:
			h.BracketedPaste = enabled
		}
	}
}

// setModifyOtherKeys handles xterm's "CSI > 4 ; Pm m": parameter 4 names
// the modifyOtherKeys resource, Pm its level (0 off, 2 = report all
// modified keys). tmux's extended-keys sends level 2. Any other "CSI >
// Pm m" (modifyKeyboard/CursorKeys/...) is ignored.
func (h *Handler) setModifyOtherKeys(params []int) {
	if len(params) == 0 || params[0] != 4 {
		return
	}
	level := 0
	if len(params) > 1 {
		level = params[1]
	}
	h.ModifyOtherKeys = level
}

// dispatchKittyKeyboard routes the kitty keyboard protocol's private CSI
// sequences, all of which end in 'u':
//
//	CSI ? u        — query current flags; respond "CSI ? flags u"
//	CSI > flags u  — push current flags, then set flags (default 0)
//	CSI < n u      — pop n entries (default 1); empty stack resets flags
//	CSI = flags;mode u — set flags per mode (1 replace, 2 set, 3 clear)
func (h *Handler) dispatchKittyKeyboard(params []int, private byte) {
	switch private {
	case '?':
		h.AppendResponse(fmt.Appendf(nil, "\x1b[?%du", KittySupportedFlags))
	case '>':
		flags := 0
		if len(params) > 0 {
			flags = params[0]
		}
		h.kittyPush(flags)
	case '<':
		n := 1
		if len(params) > 0 && params[0] > 0 {
			n = params[0]
		}
		h.kittyPop(n)
	case '=':
		h.kittySetFlags(params)
	}
}

func (h *Handler) kittyPush(flags int) {
	k := h.kitty()
	if len(k.stack) >= kittyMaxStack {
		// Full: evict the oldest entry (spec) before recording the new one.
		copy(k.stack, k.stack[1:])
		k.stack = k.stack[:len(k.stack)-1]
	}
	k.stack = append(k.stack, k.flags)
	k.flags = flags
}

func (h *Handler) kittyPop(n int) {
	k := h.kitty()
	for ; n > 0 && len(k.stack) > 0; n-- {
		k.flags = k.stack[len(k.stack)-1]
		k.stack = k.stack[:len(k.stack)-1]
	}
	if n > 0 {
		// Popped past the stack: reset all flags (spec).
		k.flags = 0
	}
}

// kittySetFlags applies "CSI = flags;mode u" against the active screen.
func (h *Handler) kittySetFlags(params []int) {
	flags := 0
	if len(params) > 0 {
		flags = params[0]
	}
	mode := 1
	if len(params) > 1 {
		mode = params[1]
	}
	k := h.kitty()
	switch mode {
	case 2:
		k.flags |= flags
	case 3:
		k.flags &^= flags
	default:
		k.flags = flags
	}
}
