package screen

func (h *Handler) applySGR(params []int, subs [][]int) {
	if len(params) == 0 {
		h.CurAttr = Attr{}
		return
	}
	for i := 0; i < len(params); i++ {
		i += h.applySGRCode(params, subs, i)
	}
}

// underlineStyles maps SGR 4's colon sub-parameter (4:0..4:5) onto
// UnderlineStyle — ECMA-48/xterm/kitty's extended-underline convention.
var underlineStyles = [...]UnderlineStyle{
	UnderlineNone, UnderlineSingle, UnderlineDouble, UnderlineCurly, UnderlineDotted, UnderlineDashed,
}

// applySGRCode applies params[i] (and, for SGR 4, its colon sub-parameter
// at subs[i] if any), returning how many further params (for the
// multi-part 38;5;n / 38;2;r;g;b / 48;... extended color forms) it also
// consumed.
func (h *Handler) applySGRCode(params []int, subs [][]int, i int) int {
	switch code := params[i]; {
	case code == 0:
		h.CurAttr = Attr{}
	case code == 1:
		h.CurAttr.Bold = true
	case code == 2:
		h.CurAttr.Dim = true
	case code == 4:
		h.CurAttr.Underline = UnderlineSingle
		if sub := subs[i]; len(sub) > 0 && sub[0] >= 0 && sub[0] < len(underlineStyles) {
			h.CurAttr.Underline = underlineStyles[sub[0]]
		}
	case code == 5:
		h.CurAttr.Blink = true
	case code == 7:
		h.CurAttr.Reverse = true
	case code == 8:
		h.CurAttr.Invisible = true
	case code == 22:
		h.CurAttr.Bold, h.CurAttr.Dim = false, false
	case code == 24:
		h.CurAttr.Underline = UnderlineNone
	case code == 25:
		h.CurAttr.Blink = false
	case code == 27:
		h.CurAttr.Reverse = false
	case code == 28:
		h.CurAttr.Invisible = false
	case code >= 30 && code <= 37:
		h.setIndexedColor(true, code-30)
	case code == 38:
		return h.applyExtendedColor(params, i, true)
	case code == 39:
		h.CurAttr.FgSet, h.CurAttr.FgIndexed = false, false
	case code >= 40 && code <= 47:
		h.setIndexedColor(false, code-40)
	case code == 48:
		return h.applyExtendedColor(params, i, false)
	case code == 49:
		h.CurAttr.BgSet, h.CurAttr.BgIndexed = false, false
	case code == 58:
		return h.applyUnderlineColor(params, subs, i)
	case code == 59:
		h.CurAttr.UnderlineColorSet, h.CurAttr.UnderlineIndexed = false, false
	case code >= 90 && code <= 97:
		h.setIndexedColor(true, code-90+8)
	case code >= 100 && code <= 107:
		h.setIndexedColor(false, code-100+8)
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
		n := params[i+2]
		if n >= 0 && n < 16 {
			h.setIndexedColor(isFg, n)
			return 2
		}
		h.setColor(isFg, palette256Value(n), palette256RGBValue(n))
		return 2
	case 2:
		if i+4 >= len(params) {
			return 1
		}
		r, g, b := params[i+2], params[i+3], params[i+4]
		h.setColor(isFg, rgbValue(r, g, b), rgbTriple(r, g, b))
		return 4
	}
	return 0
}

// applyUnderlineColor handles SGR 58 (set an explicit underline color,
// independent of Fg — see Attr.UnderlineColorSet). Real terminals emit
// this in colon form (58:5:n or 58:2::r:g:b, i.e. subs[i]) rather than
// 38/48's traditional semicolon-chained form, so that's checked first; the
// semicolon form is still accepted as a fallback, mirroring
// applyExtendedColor. Returns how many further params (semicolon form
// only — the colon form carries everything in subs[i] already) it
// consumed.
func (h *Handler) applyUnderlineColor(params []int, subs [][]int, i int) int {
	if sub := subs[i]; len(sub) > 0 {
		switch sub[0] {
		case 5:
			if len(sub) >= 2 {
				h.setUnderlineColor(sub[1])
			}
		case 2:
			if len(sub) >= 4 {
				r, g, b := sub[len(sub)-3], sub[len(sub)-2], sub[len(sub)-1]
				h.CurAttr.UnderlineColorSet, h.CurAttr.UnderlineIndexed, h.CurAttr.UnderlineRGB = true, false, rgbTriple(r, g, b)
			}
		}
		return 0
	}
	if i+1 >= len(params) {
		return 0
	}
	switch params[i+1] {
	case 5:
		if i+2 >= len(params) {
			return 1
		}
		h.setUnderlineColor(params[i+2])
		return 2
	case 2:
		if i+4 >= len(params) {
			return 1
		}
		r, g, b := params[i+2], params[i+3], params[i+4]
		h.CurAttr.UnderlineColorSet, h.CurAttr.UnderlineIndexed, h.CurAttr.UnderlineRGB = true, false, rgbTriple(r, g, b)
		return 4
	}
	return 0
}

// setUnderlineColor applies an SGR 58 "5;n"/"5:n" 256-palette underline
// color, mirroring setIndexedColor's ANSI-16-vs-256-cube split.
func (h *Handler) setUnderlineColor(n int) {
	if n >= 0 && n < 16 {
		h.CurAttr.UnderlineColorSet, h.CurAttr.UnderlineIndexed, h.CurAttr.UnderlineIdx = true, true, int8(n)
		return
	}
	h.CurAttr.UnderlineColorSet, h.CurAttr.UnderlineIndexed, h.CurAttr.UnderlineRGB = true, false, palette256RGBValue(n)
}

// setIndexedColor applies an ANSI-indexed SGR color (0-15): the palette
// slot is recorded (FgIdx/BgIdx + FgIndexed/BgIndexed) rather than
// resolved to RGB immediately, so a live theme switch can recolor this
// cell later — see Attr's doc comment. Fg/Bg (the intensity scalar the
// monochrome ramp path uses) is still resolved eagerly against the fixed
// classic-xterm table, which is intentionally theme-independent.
func (h *Handler) setIndexedColor(isFg bool, n int) {
	if isFg {
		h.CurAttr.FgSet, h.CurAttr.FgIndexed, h.CurAttr.FgIdx, h.CurAttr.Fg = true, true, int8(n), ansi16Value(n)
	} else {
		h.CurAttr.BgSet, h.CurAttr.BgIndexed, h.CurAttr.BgIdx, h.CurAttr.Bg = true, true, int8(n), ansi16Value(n)
	}
}

func (h *Handler) setColor(isFg bool, lum float32, rgb [3]float32) {
	if isFg {
		h.CurAttr.FgSet, h.CurAttr.FgIndexed, h.CurAttr.Fg, h.CurAttr.FgRGB = true, false, lum, rgb
	} else {
		h.CurAttr.BgSet, h.CurAttr.BgIndexed, h.CurAttr.Bg, h.CurAttr.BgRGB = true, false, lum, rgb
	}
}
