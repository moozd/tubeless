package font

import (
	"image"
	"math"
)

// Procedural fallbacks for common "symbol" codepoints (checks, crosses,
// arrows, stars, warning sign, media controls) that the bundled FiraCode
// Nerd Font Propo doesn't actually ship — these show up in shell prompts
// (robbyrussell's ➜ and ✗, git arrows, ★/⚠ status icons) and TUI
// decorations, and a missing glyph currently renders as an empty cell.
// Unlike the box/block/powerline sprites, these are only drawn when neither
// the loaded font nor the fallback font has a real glyph for the codepoint:
// Build records which symbol runes are genuinely missing in
// proceduralSymbols, and spriteGlyph routes only those to the geometry
// below — a font that does carry a real ➜ or ✓ keeps it.
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
	0x23FA, // ⏺ black circle for record (Claude Code's status bullet)
	0x2B24, // ⬤ black large circle
	0x26AB, // ⚫ medium black circle
	0x26A0, // ⚠ warning sign (prompt status)
	0x2605, // ★ black star
	0x2606, // ☆ white star
	0x2726, // ✦ black four-pointed star
	0x2727, // ✧ white four-pointed star
	0x23F8, // ⏸ pause (TUI media controls)
	0x23F9, // ⏹ stop (TUI media controls)
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
	case 0x23FA, 0x2B24, 0x26AB: // filled circle bullets
		pred = func(x, y float64) bool {
			u, v := toUnit(x, y)
			du, dv := u-0.5, v-0.5
			return du*du+dv*dv <= 0.5*0.5
		}
	case 0x23F8: // ⏸ pause: two side-by-side bars
		pred = func(x, y float64) bool {
			u, v := toUnit(x, y)
			return (u >= 0.25 && u <= 0.4 || u >= 0.6 && u <= 0.75) &&
				v >= 0.22 && v <= 0.78
		}
	case 0x23F9: // ⏹ stop: one filled square
		pred = func(x, y float64) bool {
			u, v := toUnit(x, y)
			return u >= 0.22 && u <= 0.78 && v >= 0.22 && v <= 0.78
		}
	case 0x26A0: // ⚠ filled triangle with the exclamation carved out
		pred = func(x, y float64) bool {
			u, v := toUnit(x, y)
			if !inTri(u, v,
				[2]float64{0.5, 0.05},
				[2]float64{0.06, 0.9},
				[2]float64{0.94, 0.9}) {
				return false
			}
			if u >= 0.45 && u <= 0.55 && v >= 0.34 && v <= 0.6 {
				return false // exclamation bar
			}
			du, dv := u-0.5, v-0.74
			return !(du*du+dv*dv <= 0.055*0.055) // exclamation dot
		}
	case 0x2605: // ★ filled 5-point star
		star := starVertices(0.5, 0.5, 0.5, 0.19, 5)
		pred = func(x, y float64) bool {
			u, v := toUnit(x, y)
			return inPoly(u, v, star)
		}
	case 0x2606: // ☆ outline 5-point star
		pred = unitStroke(toUnit, starSegments(0.5, 0.5, 0.5, 0.19, 5), halfU)
	case 0x2726: // ✦ filled 4-point star
		star := starVertices(0.5, 0.5, 0.5, 0.16, 4)
		pred = func(x, y float64) bool {
			u, v := toUnit(x, y)
			return inPoly(u, v, star)
		}
	case 0x2727: // ✧ outline 4-point star
		pred = unitStroke(toUnit, starSegments(0.5, 0.5, 0.5, 0.16, 4), halfU)
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

// starVertices returns the 2n vertices of an n-pointed star (alternating
// outer and inner radii, starting at the top) centred at (cx, cy) in unit
// space. A classic 5-point star is n=5; a 4-point sparkle (✦✧) is n=4.
func starVertices(cx, cy, rOuter, rInner float64, n int) [][2]float64 {
	out := make([][2]float64, 0, 2*n)
	for i := 0; i < 2*n; i++ {
		ang := -math.Pi/2 + float64(i)*math.Pi/float64(n)
		r := rOuter
		if i%2 == 1 {
			r = rInner
		}
		out = append(out, [2]float64{cx + r*math.Cos(ang), cy + r*math.Sin(ang)})
	}
	return out
}

// starSegments returns the closed outline segments of an n-pointed star (see
// starVertices), for unitStroke to stroke as an outline (☆✧).
func starSegments(cx, cy, rOuter, rInner float64, n int) []segment {
	verts := starVertices(cx, cy, rOuter, rInner, n)
	segs := make([]segment, 0, len(verts))
	for i := range verts {
		a, b := verts[i], verts[(i+1)%len(verts)]
		segs = append(segs, seg(a[0], a[1], b[0], b[1]))
	}
	return segs
}

// inPoly reports whether (x, y) lies inside the closed polygon verts, by
// even-odd ray casting. Used to fill the star shapes (★✦) drawn as
// alternating outer/inner vertices.
func inPoly(x, y float64, verts [][2]float64) bool {
	inside := false
	j := len(verts) - 1
	for i := range verts {
		xi, yi := verts[i][0], verts[i][1]
		xj, yj := verts[j][0], verts[j][1]
		if (yi > y) != (yj > y) && x < (xj-xi)*(y-yi)/(yj-yi)+xi {
			inside = !inside
		}
		j = i
	}
	return inside
}
