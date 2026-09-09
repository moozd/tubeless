package font

import (
	"image"
	"math"
)

// Procedural fallbacks for common "symbol" codepoints (checks, crosses,
// arrows) that the bundled FiraCode Nerd Font Propo doesn't actually ship —
// these show up in shell prompts (robbyrussell's ➜ and ✗, git arrows) and TUI
// decorations, and a missing glyph currently renders as an empty cell. Unlike
// the box/block/powerline sprites, these are only used when the loaded font
// has no glyph of its own: Build appends them to the rune list solely when
// the font lacks the codepoint, so a font that does carry a real ➜ keeps it.
//
// Each shape is drawn in a unit [0,1] coordinate space that is mapped with a
// single uniform scale into a centred icon box that fills most of the cell
// width and about two-thirds of its height — the same way the font's own
// symbol glyphs sit (inset, mid-cell) rather than full-bleed, so a check or
// cross doesn't dwarf the surrounding prompt text. Uniform scaling keeps the
// shapes from skewing on wide/narrow cells.

var symbolRunes = []rune{
	0x2713, // ✓ check mark
	0x2714, // ✔ heavy check mark
	0x2715, // ✕ multiplication x
	0x2716, // ✖ heavy multiplication x
	0x2717, // ✗ ballot x
	0x2718, // ✘ heavy ballot x
	0x2794, // ➔ heavy wide-headed right arrow
	0x279C, // ➜ heavy round-tipped right arrow
	0x279E, // ➞ heavy right arrow
	0x27A4, // ➤ heavy black right-pointing pointer
	0x27A6, // ➦ heavy black curved down and rightward arrow (git)
}

func isSymbolSprite(r rune) bool {
	for _, p := range symbolRunes {
		if r == p {
			return true
		}
	}
	return false
}

func drawSymbolSprite(r rune, img *image.Alpha, gx, gy, cellW, cellH int) {
	// Icon box: as wide as the cell allows, ~2/3 of the cell height, centred.
	side := math.Min(float64(cellW)*0.92, float64(cellH)*0.62)
	ox := (float64(cellW) - side) / 2
	oy := (float64(cellH) - side) / 2

	// Pixel stroke width for this glyph, expressed in unit space.
	heavy := r == 0x2714 || r == 0x2716 || r == 0x2718
	halfU := symThickPx(cellW, cellH, heavy) / side / 2

	// toUnit converts a sampled (x, y) into icon unit coordinates.
	toUnit := func(x, y float64) (u, v float64) {
		return (x - ox) / side, (y - oy) / side
	}

	var pred inside
	switch r {
	case 0x2713, 0x2714: // ✓ ✔
		segs := []segment{
			seg(0.1, 0.58, 0.42, 0.8),
			seg(0.42, 0.8, 0.94, 0.16),
		}
		pred = unitStroke(toUnit, segs, halfU)
	case 0x2715, 0x2716, 0x2717, 0x2718: // ✕ ✖ ✗ ✘
		pred = unitStroke(toUnit, []segment{
			seg(0.16, 0.16, 0.84, 0.84),
			seg(0.84, 0.16, 0.16, 0.84),
		}, halfU)
	case 0x2794, 0x279C, 0x279E, 0x27A4, 0x27A6: // filled right arrows
		pred = func(x, y float64) bool {
			u, v := toUnit(x, y)
			if u >= 0.06 && u <= 0.5 && v >= 0.36 && v <= 0.64 {
				return true // tail
			}
			// head: right-pointing triangle
			return inTri(u, v,
				[2]float64{0.5, 0.08},
				[2]float64{0.96, 0.5},
				[2]float64{0.5, 0.92})
		}
	default:
		return
	}
	raster(img, gx, gy, cellW, cellH, pred)
}

// symThickPx derives a symbol stroke width from the box-drawing light width
// (heavy symbols double it), in atlas pixels.
func symThickPx(cellW, cellH int, heavy bool) float64 {
	t := float64(lineThickness(cellW, cellH))
	if heavy {
		return t * 2
	}
	return t
}

// unitStroke is a polyline stroked in unit space; halfU is the half-width in
// unit space (see drawSymbolSprite). Samples outside the unit box still get a
// correct distance, so strokes that run to the box edge stay crisp.
func unitStroke(toUnit func(x, y float64) (u, v float64), segs []segment, halfU float64) inside {
	return func(x, y float64) bool {
		u, v := toUnit(x, y)
		for _, s := range segs {
			if distSeg(u, v, s.ax, s.ay, s.bx, s.by) <= halfU {
				return true
			}
		}
		return false
	}
}
