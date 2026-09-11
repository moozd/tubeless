package font

import "image"

// Box drawing U+2500-U+257F, ported from Ghostty's sprite/draw/box.zig:
// strokes run through the cell's middle lines and reach the edges the
// character calls for, overshooting `over` px past them so neighbouring cells
// join seamlessly. Weights: light = lineThickness, heavy = 2x, double = two
// light strokes straddling the centre line with a light gap between them. The
// junction stop rules decide how far a stroke reaches so one weight meeting a
// heavier/double perpendicular butts flush instead of poking through it.

type lineWeight uint8

const (
	lwNone lineWeight = iota
	lwLight
	lwHeavy
	lwDouble
)

type lines struct{ up, down, left, right lineWeight }

func drawBoxSprite(r rune, img *image.Alpha, gx, gy, cellW, cellH int) {
	thick := lineThickness(cellW, cellH)

	switch {
	case isHDash(r):
		raster(img, gx, gy, cellW, cellH, dashHoriz(cellW, cellH, dashKind(r)))
	case isVDash(r):
		raster(img, gx, gy, cellW, cellH, dashVert(cellW, cellH, dashKind(r)))
	case r >= 0x256D && r <= 0x2570: // ╭ ╮ ╯ ╰ rounded corners
		raster(img, gx, gy, cellW, cellH, arcPred(cellW, cellH, float64(thick), int(r-0x256D)))
	case r >= 0x2571 && r <= 0x2573: // ╱ ╲ ╳ diagonals
		raster(img, gx, gy, cellW, cellH, diagPred(cellW, cellH, float64(thick), int(r-0x2571)))
	default:
		if ln, ok := boxLines[r]; ok {
			raster(img, gx, gy, cellW, cellH, boxJunctionPred(cellW, cellH, thick, ln))
		}
	}
}

func mk(up, down, left, right lineWeight) lines {
	return lines{up: up, down: down, left: left, right: right}
}

// corner4 fills base..base+3 with the corner whose two arm positions sweep
// every light/heavy combination in Ghostty's order.
func corner4(m map[rune]lines, base rune, armA func(lines, lineWeight) lines, armB func(lines, lineWeight) lines) {
	w := lwLight
	h := lwHeavy
	for _, p := range [4][2]lineWeight{{w, w}, {w, h}, {h, w}, {h, h}} {
		ln := armA(armB(lines{}, p[0]), p[1])
		m[base] = ln
		base++
	}
}

// eight fills base..base+7 with the tee weights from `order`.
func eight(m map[rune]lines, base rune, order [][3]lineWeight, apply func(lines, [3]lineWeight) lines) {
	for _, t := range order {
		m[base] = apply(lines{}, t)
		base++
	}
}

var boxLines = func() map[rune]lines {
	m := map[rune]lines{}
	L, H := lwLight, lwHeavy

	m[0x2500] = mk(lwNone, lwNone, L, L) // ─
	m[0x2501] = mk(lwNone, lwNone, H, H) // ━
	m[0x2502] = mk(L, L, lwNone, lwNone) // │
	m[0x2503] = mk(H, H, lwNone, lwNone) // ┃
	// Dashes (0x2504-0x250B, 0x254C-0x254F) render via dashHoriz/dashVert.
	for i := 0x2504; i <= 0x250B; i++ {
		m[rune(i)] = lines{}
	}
	for i := 0x254C; i <= 0x254F; i++ {
		m[rune(i)] = lines{}
	}

	// 0x250C ┌ : down+right through all weights.
	corner4(m, 0x250C, func(l lines, v lineWeight) lines { l.down = v; return l },
		func(l lines, v lineWeight) lines { l.right = v; return l })
	// 0x2510 ┐ : down+left.
	corner4(m, 0x2510, func(l lines, v lineWeight) lines { l.down = v; return l },
		func(l lines, v lineWeight) lines { l.left = v; return l })
	// 0x2514 └ : up+right.
	corner4(m, 0x2514, func(l lines, v lineWeight) lines { l.up = v; return l },
		func(l lines, v lineWeight) lines { l.right = v; return l })
	// 0x2518 ┘ : up+left.
	corner4(m, 0x2518, func(l lines, v lineWeight) lines { l.up = v; return l },
		func(l lines, v lineWeight) lines { l.left = v; return l })

	// ├┤ tees (0x251C, 0x2524): up/down sweep in Ghostty's order, arm fixed.
	udOrder := [][3]lineWeight{
		{L, L, L}, {L, L, H}, {H, L, L}, {L, H, L}, {H, H, L}, {H, L, H}, {L, H, H}, {H, H, H},
	}
	eight(m, 0x251C, udOrder, func(l lines, t [3]lineWeight) lines {
		return mk(t[0], t[1], lwNone, t[2])
	})
	eight(m, 0x2524, udOrder, func(l lines, t [3]lineWeight) lines {
		return mk(t[0], t[1], t[2], lwNone)
	})

	// ┬┴ tees (0x252C, 0x2534): stem + left/right sweep in Ghostty's order.
	lrOrder := [][3]lineWeight{
		{L, L, L}, {L, H, L}, {L, L, H}, {L, H, H}, {H, L, L}, {H, H, L}, {H, L, H}, {H, H, H},
	}
	eight(m, 0x252C, lrOrder, func(l lines, t [3]lineWeight) lines {
		return mk(lwNone, t[0], t[1], t[2])
	})
	eight(m, 0x2534, lrOrder, func(l lines, t [3]lineWeight) lines {
		return mk(t[0], lwNone, t[1], t[2])
	})

	// Crosses 0x253C-0x254B (irregular Ghostty ordering).
	cross := [][4]lineWeight{
		{L, L, L, L}, {L, L, H, L}, {L, L, L, H}, {L, L, H, H},
		{H, L, L, L}, {L, H, L, L}, {H, H, L, L}, {H, L, H, L},
		{H, L, L, H}, {L, H, H, L}, {L, H, L, H}, {H, L, H, H},
		{L, H, H, H}, {H, H, H, L}, {H, H, L, H}, {H, H, H, H},
	}
	for i, t := range cross {
		m[rune(0x253C+i)] = mk(t[0], t[1], t[2], t[3])
	}

	// ═ ║ double lines.
	m[0x2550] = mk(lwNone, lwNone, lwDouble, lwDouble)
	m[0x2551] = mk(lwDouble, lwDouble, lwNone, lwNone)
	// Double corners (light/double mixes), per Ghostty:
	m[0x2552] = mk(lwNone, L, lwNone, lwDouble) // ╒
	m[0x2553] = mk(lwNone, lwDouble, lwNone, L) // ╓
	m[0x2554] = mk(lwNone, lwDouble, lwNone, lwDouble)
	m[0x2555] = mk(lwNone, L, lwDouble, lwNone) // ╕
	m[0x2556] = mk(lwNone, lwDouble, L, lwNone) // ╖
	m[0x2557] = mk(lwNone, lwDouble, lwDouble, lwNone)
	m[0x2558] = mk(L, lwNone, lwNone, lwDouble) // ╘
	m[0x2559] = mk(lwDouble, lwNone, lwNone, L) // ╙
	m[0x255A] = mk(lwDouble, lwNone, lwNone, lwDouble)
	m[0x255B] = mk(L, lwNone, lwDouble, lwNone) // ╛
	m[0x255C] = mk(lwDouble, lwNone, L, lwNone) // ╜
	m[0x255D] = mk(lwDouble, lwNone, lwDouble, lwNone)

	// 0x255E-0x2563 ├┤ variants (up/down vs double right/left).
	m[0x255E] = mk(L, L, lwNone, lwDouble)
	m[0x255F] = mk(lwDouble, lwDouble, lwNone, L)
	m[0x2560] = mk(lwDouble, lwDouble, lwNone, lwDouble)
	m[0x2561] = mk(L, L, lwDouble, lwNone)
	m[0x2562] = mk(lwDouble, lwDouble, L, lwNone)
	m[0x2563] = mk(lwDouble, lwDouble, lwDouble, lwNone)
	// 0x2564-0x2569 ┬┴ variants.
	m[0x2564] = mk(lwNone, L, lwDouble, lwDouble)
	m[0x2565] = mk(lwNone, lwDouble, L, L)
	m[0x2566] = mk(lwNone, lwDouble, lwDouble, lwDouble)
	m[0x2567] = mk(L, lwNone, lwDouble, lwDouble)
	m[0x2568] = mk(lwDouble, lwNone, L, L)
	m[0x2569] = mk(lwDouble, lwNone, lwDouble, lwDouble)
	// Crosses.
	m[0x256A] = mk(L, L, lwDouble, lwDouble)
	m[0x256B] = mk(lwDouble, lwDouble, L, L)
	m[0x256C] = mk(lwDouble, lwDouble, lwDouble, lwDouble)
	// One-sided strokes 0x2574-0x257F.
	m[0x2574] = mk(lwNone, lwNone, L, lwNone)
	m[0x2575] = mk(L, lwNone, lwNone, lwNone)
	m[0x2576] = mk(lwNone, lwNone, lwNone, L)
	m[0x2577] = mk(lwNone, L, lwNone, lwNone)
	m[0x2578] = mk(lwNone, lwNone, H, lwNone)
	m[0x2579] = mk(H, lwNone, lwNone, lwNone)
	m[0x257A] = mk(lwNone, lwNone, lwNone, H)
	m[0x257B] = mk(lwNone, H, lwNone, lwNone)
	m[0x257C] = mk(lwNone, lwNone, L, H)
	m[0x257D] = mk(L, H, lwNone, lwNone)
	m[0x257E] = mk(lwNone, lwNone, H, L)
	m[0x257F] = mk(H, L, lwNone, lwNone)

	// ⎿ dentistry symbol light down and horizontal: visually the same
	// up+right corner as └ (0x2514) at terminal cell sizes, and used the
	// same way by several CLIs (Claude Code among them) as a sub-item
	// tree connector — most fonts, ours included, don't carry the
	// dentistry block at all, so without this it silently renders as an
	// empty cell instead of the connector line.
	m[0x23BF] = mk(L, lwNone, lwNone, L)

	return m
}()

// boxJunctionPred returns the coverage predicate for a junction character.
func boxJunctionPred(cellW, cellH, thick int, ln lines) inside {
	light := thick
	heavy := thick * 2

	w, h := float64(cellW), float64(cellH)
	lw, hw := float64(light), float64(heavy)

	hLightTop := (h - lw) / 2
	hLightBot := hLightTop + lw
	hHeavyTop := (h - hw) / 2
	hHeavyBot := hHeavyTop + hw
	hDoubleTop := hLightTop - lw
	hDoubleBot := hLightBot + lw

	vLightLeft := (w - lw) / 2
	vLightRight := vLightLeft + lw
	vHeavyLeft := (w - hw) / 2
	vHeavyRight := vHeavyLeft + hw
	vDoubleLeft := vLightLeft - lw
	vDoubleRight := vLightRight + lw

	upEnd := vStop(ln, hHeavyBot, hDoubleBot, hLightBot, hLightTop)
	downStart := vStop(ln, hHeavyTop, hDoubleTop, hLightTop, hLightBot)
	leftEnd := hStop(ln, vHeavyRight, vDoubleRight, vLightRight, vLightLeft)
	rightStart := hStop(ln, vHeavyLeft, vDoubleLeft, vLightLeft, vLightRight)

	rs := []rect{}

	switch ln.up {
	case lwLight:
		rs = append(rs, expand(cellW, cellH, rect{vLightLeft, 0, vLightRight, upEnd}))
	case lwHeavy:
		rs = append(rs, expand(cellW, cellH, rect{vHeavyLeft, 0, vHeavyRight, upEnd}))
	case lwDouble:
		lb, rb := upEnd, upEnd
		if ln.left == lwDouble {
			lb = hLightTop
		}
		if ln.right == lwDouble {
			rb = hLightTop
		}
		rs = append(rs, expand(cellW, cellH, rect{vDoubleLeft, 0, vLightLeft, lb}))
		rs = append(rs, expand(cellW, cellH, rect{vLightRight, 0, vDoubleRight, rb}))
	}
	switch ln.down {
	case lwLight:
		rs = append(rs, expand(cellW, cellH, rect{vLightLeft, downStart, vLightRight, h}))
	case lwHeavy:
		rs = append(rs, expand(cellW, cellH, rect{vHeavyLeft, downStart, vHeavyRight, h}))
	case lwDouble:
		lt, rt := downStart, downStart
		if ln.left == lwDouble {
			lt = hLightBot
		}
		if ln.right == lwDouble {
			rt = hLightBot
		}
		rs = append(rs, expand(cellW, cellH, rect{vDoubleLeft, lt, vLightLeft, h}))
		rs = append(rs, expand(cellW, cellH, rect{vLightRight, rt, vDoubleRight, h}))
	}
	switch ln.left {
	case lwLight:
		rs = append(rs, expand(cellW, cellH, rect{0, hLightTop, leftEnd, hLightBot}))
	case lwHeavy:
		rs = append(rs, expand(cellW, cellH, rect{0, hHeavyTop, leftEnd, hHeavyBot}))
	case lwDouble:
		tr, br := leftEnd, leftEnd
		if ln.up == lwDouble {
			tr = vLightLeft
		}
		if ln.down == lwDouble {
			br = vLightLeft
		}
		rs = append(rs, expand(cellW, cellH, rect{0, hDoubleTop, tr, hLightTop}))
		rs = append(rs, expand(cellW, cellH, rect{0, hLightBot, br, hDoubleBot}))
	}
	switch ln.right {
	case lwLight:
		rs = append(rs, expand(cellW, cellH, rect{rightStart, hLightTop, w, hLightBot}))
	case lwHeavy:
		rs = append(rs, expand(cellW, cellH, rect{rightStart, hHeavyTop, w, hHeavyBot}))
	case lwDouble:
		tl, bl := rightStart, rightStart
		if ln.up == lwDouble {
			tl = vLightRight
		}
		if ln.down == lwDouble {
			bl = vLightRight
		}
		rs = append(rs, expand(cellW, cellH, rect{tl, hDoubleTop, w, hLightTop}))
		rs = append(rs, expand(cellW, cellH, rect{bl, hLightBot, w, hDoubleBot}))
	}

	return union(rs)
}

// vStop is Ghostty's up_bottom/down_top rule: how far a vertical (up/down)
// stroke reaches toward the junction, given the perpendicular left/right
// strokes and the candidate stop positions.
func vStop(ln lines, heavyEnd, doubleEnd, lightEnd, lightTop float64) float64 {
	if ln.left == lwHeavy || ln.right == lwHeavy {
		return heavyEnd
	}
	if ln.left != ln.right || ln.down == ln.up {
		if ln.left == lwDouble || ln.right == lwDouble {
			return doubleEnd
		}
		return lightEnd
	}
	if ln.left == lwNone && ln.right == lwNone {
		return lightEnd
	}
	return lightTop
}

// hStop mirrors vStop for horizontal strokes, keyed off the up/down strokes.
func hStop(ln lines, heavyEnd, doubleEnd, lightEnd, lightLeft float64) float64 {
	if ln.up == lwHeavy || ln.down == lwHeavy {
		return heavyEnd
	}
	if ln.up != ln.down || ln.right == ln.left {
		if ln.up == lwDouble || ln.down == lwDouble {
			return doubleEnd
		}
		return lightEnd
	}
	if ln.up == lwNone && ln.down == lwNone {
		return lightEnd
	}
	return lightLeft
}
