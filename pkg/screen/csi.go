package screen

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
		h.restoreCursor()
	case 'm':
		// A private marker (e.g. xterm's "CSI > 4;2 m" modifyOtherKeys
		// negotiation) means this isn't SGR at all, just a sequence that
		// happens to also end in 'm' — applying it as SGR misreads its
		// params as color/attribute codes.
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
