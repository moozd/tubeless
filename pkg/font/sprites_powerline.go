package font

import "image"

// The straight-edged Powerline glyphs (U+E0B0-U+E0D4 region) that Ghostty
// draws geometrically: filled triangles (segment separators), chevron strokes
// and the branch/arrow pieces made of triangles. Curved powerline glyphs (the
//  and  C shapes, the notched ones) are left to the font, which renders
// them well. These glyphs want to exactly abut the cell's left/right edges —
// how a powerline prompt separates a coloured segment from the next — which
// is exactly what a font's em-box padding breaks, so we generate them.

var powerlineRunes = []rune{
	0xE0B0, 0xE0B1, 0xE0B2, 0xE0B3,
	0xE0B8, 0xE0B9, 0xE0BA, 0xE0BB,
	0xE0BC, 0xE0BD, 0xE0BE, 0xE0BF,
}

func isPowerlineSprite(r rune) bool {
	for _, p := range powerlineRunes {
		if r == p {
			return true
		}
	}
	return false
}

func drawPowerlineSprite(r rune, img *image.Alpha, gx, gy, cellW, cellH int) {
	w := float64(cellW)
	h := float64(cellH)
	thick := float64(lineThickness(cellW, cellH))
	ov := float64(over)

	var pred inside
	switch r {
	case 0xE0B0: // full right-pointing wedge
		pred = tri([]float64{-ov, 0, w + ov, h / 2, -ov, h})
	case 0xE0B2: // full left-pointing wedge
		pred = tri([]float64{w + ov, 0, -ov, h / 2, w + ov, h})
	case 0xE0B1: // chevron outline pointing right
		pred = stroke([]segment{seg(-ov, 0, w, h/2), seg(w, h/2, -ov, h)}, thick/2)
	case 0xE0B3: // chevron outline pointing left
		pred = stroke([]segment{seg(w+ov, 0, 0, h/2), seg(0, h/2, w+ov, h)}, thick/2)
	case 0xE0B8: // upper-left to lower-right half
		pred = tri([]float64{0, 0, w + ov, h + ov, 0, h + ov})
	case 0xE0B9: // diagonal, same as ╲
		pred = diagPred(cellW, cellH, thick, 1)
	case 0xE0BA: // upper-right filled wedge (points down-left)
		pred = tri([]float64{w + ov, 0, w + ov, h + ov, 0, h + ov})
	case 0xE0BB: // diagonal, same as ╱
		pred = diagPred(cellW, cellH, thick, 0)
	case 0xE0BC: // lower-right filled wedge
		pred = tri([]float64{0, -ov, w + ov, -ov, 0, h + ov})
	case 0xE0BD: // diagonal, same as ╱
		pred = diagPred(cellW, cellH, thick, 0)
	case 0xE0BE: // upper-right filled wedge (points down-left), tall
		pred = tri([]float64{0, -ov, w + ov, -ov, w + ov, h + ov})
	case 0xE0BF: // diagonal, same as ╲
		pred = diagPred(cellW, cellH, thick, 1)
	default:
		return
	}
	raster(img, gx, gy, cellW, cellH, pred)
}

// tri builds an inside predicate for the filled triangle with vertices given
// as a flat [x0,y0, x1,y1, x2,y2] list.
func tri(v []float64) inside {
	a := [2]float64{v[0], v[1]}
	b := [2]float64{v[2], v[3]}
	c := [2]float64{v[4], v[5]}
	return func(x, y float64) bool { return inTri(x, y, a, b, c) }
}
