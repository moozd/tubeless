package screen

func (h *Handler) applySGR(params []int) {
	if len(params) == 0 {
		h.CurAttr = Attr{}
		return
	}
	for i := 0; i < len(params); i++ {
		i += h.applySGRCode(params, i)
	}
}

// applySGRCode applies params[i], returning how many further params (for
// the multi-part 38;5;n / 38;2;r;g;b / 48;... extended color forms) it
// also consumed.
func (h *Handler) applySGRCode(params []int, i int) int {
	switch code := params[i]; {
	case code == 0:
		h.CurAttr = Attr{}
	case code == 1:
		h.CurAttr.Bold = true
	case code == 2:
		h.CurAttr.Dim = true
	case code == 4:
		h.CurAttr.Underline = true
	case code == 5:
		h.CurAttr.Blink = true
	case code == 7:
		h.CurAttr.Reverse = true
	case code == 8:
		h.CurAttr.Invisible = true
	case code == 22:
		h.CurAttr.Bold, h.CurAttr.Dim = false, false
	case code == 24:
		h.CurAttr.Underline = false
	case code == 25:
		h.CurAttr.Blink = false
	case code == 27:
		h.CurAttr.Reverse = false
	case code == 28:
		h.CurAttr.Invisible = false
	case code >= 30 && code <= 37:
		h.CurAttr.FgSet, h.CurAttr.Fg = true, ansi16Value(code-30)
	case code == 38:
		return h.applyExtendedColor(params, i, true)
	case code == 39:
		h.CurAttr.FgSet = false
	case code >= 40 && code <= 47:
		h.CurAttr.BgSet, h.CurAttr.Bg = true, ansi16Value(code-40)
	case code == 48:
		return h.applyExtendedColor(params, i, false)
	case code == 49:
		h.CurAttr.BgSet = false
	case code >= 90 && code <= 97:
		h.CurAttr.FgSet, h.CurAttr.Fg = true, ansi16Value(code-90+8)
	case code >= 100 && code <= 107:
		h.CurAttr.BgSet, h.CurAttr.Bg = true, ansi16Value(code-100+8)
	}
	return 0
}

// applyExtendedColor handles "38;5;n" / "38;2;r;g;b" (isFg) and their
// "48;..." background equivalents, starting at params[i] (the 38/48
// itself). Returns how many params beyond params[i] were consumed.
func (h *Handler) applyExtendedColor(params []int, i int, isFg bool) int {
	if i+1 >= len(params) {
		return 0
	}
	switch params[i+1] {
	case 5:
		if i+2 >= len(params) {
			return 1
		}
		h.setColor(isFg, palette256Value(params[i+2]))
		return 2
	case 2:
		if i+4 >= len(params) {
			return 1
		}
		h.setColor(isFg, rgbValue(params[i+2], params[i+3], params[i+4]))
		return 4
	}
	return 0
}

func (h *Handler) setColor(isFg bool, lum float32) {
	if isFg {
		h.CurAttr.FgSet, h.CurAttr.Fg = true, lum
	} else {
		h.CurAttr.BgSet, h.CurAttr.Bg = true, lum
	}
}
