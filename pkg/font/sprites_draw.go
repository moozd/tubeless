package font

import "math"

// Dashed box lines, rounded corners (arcs) and diagonals — the remaining
// U+2500-U+257F members after the rect-junction characters in sprites_box.go.

// dashK describes one dashed box line.
type dashK struct {
	vertical bool
	count    int
	heavy    bool
}

func isHDash(r rune) bool {
	switch r {
	case 0x2504, 0x2505, 0x2508, 0x2509, 0x254C, 0x254D:
		return true
	}
	return false
}

func isVDash(r rune) bool {
	switch r {
	case 0x2506, 0x2507, 0x250A, 0x250B, 0x254E, 0x254F:
		return true
	}
	return false
}

func dashKind(r rune) dashK {
	heavy := false
	switch r {
	case 0x2505, 0x2507, 0x2509, 0x250B, 0x254D, 0x254F:
		heavy = true
	}
	count := 3
	if r >= 0x2508 && r <= 0x250B {
		count = 4
	}
	if r >= 0x254C {
		count = 2
	}
	return dashK{vertical: isVDash(r), count: count, heavy: heavy}
}

func dashWidth(light int, k dashK) int {
	if k.heavy {
		return light * 2
	}
	return light
}

// dashHoriz renders a dashed horizontal line at the cell's vertical centre,
// with half-size gaps at both ends so adjacent cells tile into a consistent
// dash/gap rhythm (Ghostty's dashHorizontal).
func dashHoriz(cellW, cellH int, k dashK) inside {
	thick := dashWidth(lineThickness(cellW, cellH), k)
	if cellW < k.count*2 {
		// Too small for the pattern; fall back to a solid line.
		return union([]rect{expand(cellW, cellH, rect{0, float64((cellH - thick) / 2), float64(cellW), float64((cellH-thick)/2 + thick)})})
	}
	gap := min(max(2, thick), cellW/(2*k.count))
	remain := cellW - k.count*gap
	dash := remain / k.count
	extra := remain % k.count
	y := float64((cellH - thick) / 2)
	yt := float64(thick)

	var rs []rect
	x := gap / 2
	for range k.count {
		d := dash
		if extra > 0 {
			extra--
			d++
		}
		rs = append(rs, rect{float64(x), y, float64(x + d), y + yt})
		x += d + gap
	}
	return union(rs)
}

// dashVert mirrors dashHoriz for vertical lines; the pattern starts with a
// dash at the cell's top and leaves the rhythm-closing gap at the bottom
// (Ghostty's dashVertical), which tiles cleanly into the next cell below.
func dashVert(cellW, cellH int, k dashK) inside {
	thick := dashWidth(lineThickness(cellW, cellH), k)
	if cellH < k.count*2 {
		return union([]rect{expand(cellW, cellH, rect{float64((cellW - thick) / 2), 0, float64((cellW-thick)/2 + thick), float64(cellH)})})
	}
	gap := min(max(2, thick), cellH/(2*k.count))
	remain := cellH - k.count*gap
	dash := remain / k.count
	extra := remain % k.count
	x := float64((cellW - thick) / 2)
	xt := float64(thick)

	var rs []rect
	y := 0
	for range k.count {
		d := dash
		if extra > 0 {
			extra--
			d++
		}
		rs = append(rs, rect{x, float64(y), x + xt, float64(y + d)})
		y += d + gap
	}
	return union(rs)
}

// arcPred renders the four rounded-corner characters ╭ ╮ ╯ ╰ (corner 0..3).
// Each is the corresponding sharp corner piece — two bar halves meeting at
// the cell centre, one reaching the bottom/top edge and one the left/right
// edge (see boxJunctionPred) — with the joint rounded by a small quarter-disc
// fillet on the box-interior side of the elbow, exactly how a font draws
// them: the two bars are pulled back from the joint by the fillet radius and
// the wedge between them is filled with a quarter circle. corner order:
// 0=╭ (bottom+right arms), 1=╮ (bottom+left), 2=╯ (top+left), 3=╰ (top+right).
func arcPred(cellW, cellH int, thick float64, corner int) inside {
	cx := float64(cellW) / 2
	cy := float64(cellH) / 2
	half := thick / 2
	// Fillet radius: a little more than the bar width, so the round reads at
	// display size but stays a corner, not a sweep across the cell.
	rho := thick * 1.5

	bar := rect{}
	switch corner {
	case 0: // ╭ ┌-round: down + right arms.
		bar.x0 = cx - half
		bar.x1 = cx + half
		bar.y0 = cy + rho
		bar.y1 = float64(cellH) + float64(over)
		hBar := rect{cx + rho, cy - half, float64(cellW) + float64(over), cy + half}
		return func(x, y float64) bool {
			return bar.contains(x, y) || hBar.contains(x, y) ||
				inCornerDisc(cx, cy, rho, x, y, +1, +1)
		}
	case 1: // ╮ ┐-round: down + left arms.
		bar.x0 = cx - half
		bar.x1 = cx + half
		bar.y0 = cy + rho
		bar.y1 = float64(cellH) + float64(over)
		hBar := rect{-float64(over), cy - half, cx - rho, cy + half}
		return func(x, y float64) bool {
			return bar.contains(x, y) || hBar.contains(x, y) ||
				inCornerDisc(cx, cy, rho, x, y, -1, +1)
		}
	case 2: // ╯ ┘-round: up + left arms.
		bar.x0 = cx - half
		bar.x1 = cx + half
		bar.y0 = -float64(over)
		bar.y1 = cy - rho
		hBar := rect{-float64(over), cy - half, cx - rho, cy + half}
		return func(x, y float64) bool {
			return bar.contains(x, y) || hBar.contains(x, y) ||
				inCornerDisc(cx, cy, rho, x, y, -1, -1)
		}
	default: // 3 ╰ └-round: up + right arms.
		bar.x0 = cx - half
		bar.x1 = cx + half
		bar.y0 = -float64(over)
		bar.y1 = cy - rho
		hBar := rect{cx + rho, cy - half, float64(cellW) + float64(over), cy + half}
		return func(x, y float64) bool {
			return bar.contains(x, y) || hBar.contains(x, y) ||
				inCornerDisc(cx, cy, rho, x, y, +1, -1)
		}
	}
}

// inCornerDisc reports whether (x, y) lies in the quarter-disc of radius rho
// centred at (cx, cy) that fills the wedge between the two arms of a corner
// (sx, sy are the signs of the wedge quadrant).
func inCornerDisc(cx, cy, rho, x, y float64, sx, sy int) bool {
	dx := x - cx
	dy := y - cy
	if sx > 0 && dx < 0 || sx < 0 && dx > 0 {
		return false
	}
	if sy > 0 && dy < 0 || sy < 0 && dy > 0 {
		return false
	}
	return dx*dx+dy*dy <= rho*rho
}

// diagPred renders ╱ (0), ╲ (1) and ╳ (2) as light-thickness centre lines.
func diagPred(cellW, cellH int, thick float64, kind int) inside {
	w := float64(cellW)
	h := float64(cellH)
	sx := math.Min(1, w/h)
	sy := math.Min(1, h/w)
	half := thick / 2

	var ls []segment
	if kind == 0 || kind == 2 { // ╱ upper-right to lower-left
		ls = append(ls, seg(w+0.5*sx, -0.5*sy, -0.5*sx, h+0.5*sy))
	}
	if kind == 1 || kind == 2 { // ╲ upper-left to lower-right
		ls = append(ls, seg(-0.5*sx, -0.5*sy, w+0.5*sx, h+0.5*sy))
	}
	return func(x, y float64) bool {
		for _, s := range ls {
			if distSeg(x, y, s.ax, s.ay, s.bx, s.by) <= half {
				return true
			}
		}
		return false
	}
}
