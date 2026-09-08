package screen

func (h *Handler) CSIDispatch(final byte, params []int, intermediates []byte, private byte) {
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
		h.applySGR(params)
	case 'r':
		h.SetScrollRegion(param(params, 0, 1)-1, param(params, 1, h.Rows)-1)
	case 'h':
		h.setMode(private, params, true)
	case 'l':
		h.setMode(private, params, false)
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
		}
	}
}
