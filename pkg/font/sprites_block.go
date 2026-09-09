package font

import "image"

// Block elements U+2580-U+259F: upper/lower halves, the 1/8th blocks, the
// quadrant blocks, and the three shaded blocks ░▒▓. Half/eighth/quadrant
// pieces are plain rectangles anchored to the cell; a side that coincides
// with a cell boundary is overshot so neighbouring blocks join seamlessly.
// Shading uses a fixed 4x4 ordered dither so the mipmapped texture reads as
// a uniform tint at display size rather than coarse pixels.

type fracRect struct{ x0, y0, x1, y1 float64 }

func drawBlockSprite(r rune, img *image.Alpha, gx, gy, cellW, cellH int) {
	w, h := float64(cellW), float64(cellH)
	rects := func(fs ...fracRect) inside {
		rs := make([]rect, 0, len(fs))
		for _, f := range fs {
			r := rect{f.x0 * w, f.y0 * h, f.x1 * w, f.y1 * h}
			rs = append(rs, expand(cellW, cellH, r))
		}
		return union(rs)
	}

	if fr, ok := blockSingleRect(r); ok {
		raster(img, gx, gy, cellW, cellH, rects(fr))
		return
	}

	var pred inside
	switch r {
	case 0x2591:
		pred = shadePred(0.25)
	case 0x2592:
		pred = shadePred(0.5)
	case 0x2593:
		pred = shadePred(0.75)
	case 0x2599:
		pred = rects(fracRect{0, 0, 0.5, 0.5}, fracRect{0, 0.5, 0.5, 1}, fracRect{0.5, 0.5, 1, 1}) // ▙
	case 0x259A:
		pred = rects(fracRect{0, 0, 0.5, 0.5}, fracRect{0.5, 0.5, 1, 1}) // ▚
	case 0x259B:
		pred = rects(fracRect{0, 0, 0.5, 0.5}, fracRect{0.5, 0, 1, 0.5}, fracRect{0, 0.5, 0.5, 1}) // ▛
	case 0x259C:
		pred = rects(fracRect{0, 0, 0.5, 0.5}, fracRect{0.5, 0, 1, 0.5}, fracRect{0.5, 0.5, 1, 1}) // ▜
	case 0x259E:
		pred = rects(fracRect{0.5, 0, 1, 0.5}, fracRect{0, 0.5, 0.5, 1}) // ▞
	case 0x259F:
		pred = rects(fracRect{0.5, 0, 1, 0.5}, fracRect{0, 0.5, 0.5, 1}, fracRect{0.5, 0.5, 1, 1}) // ▟
	default:
		return
	}
	raster(img, gx, gy, cellW, cellH, pred)
}

// blockSingleRect returns the one fractional sub-rect a block-element rune
// fills, for every U+2580-U+259F codepoint expressible as a single
// axis-aligned rectangle. ok is false for the multi-rect quadrant unions
// (▙▚▛▜▞▟) and the three shaded fills (░▒▓), which stay atlas-rasterized
// above. BlockRect exposes this to pkg/render, which draws these runes
// procedurally instead of sampling the atlas so their corners can round
// against real neighbor cells.
func blockSingleRect(r rune) (fracRect, bool) {
	switch r {
	case 0x2580:
		return fracRect{0, 0, 1, 0.5}, true // ▀
	case 0x2581:
		return fracRect{0, 0.875, 1, 1}, true
	case 0x2582:
		return fracRect{0, 0.75, 1, 1}, true
	case 0x2583:
		return fracRect{0, 0.625, 1, 1}, true
	case 0x2584:
		return fracRect{0, 0.5, 1, 1}, true // ▄
	case 0x2585:
		return fracRect{0, 0.375, 1, 1}, true
	case 0x2586:
		return fracRect{0, 0.25, 1, 1}, true
	case 0x2587:
		return fracRect{0, 0.125, 1, 1}, true
	case 0x2588:
		return fracRect{0, 0, 1, 1}, true // █
	case 0x2589:
		return fracRect{0, 0, 0.125, 1}, true
	case 0x258A:
		return fracRect{0, 0, 0.25, 1}, true
	case 0x258B:
		return fracRect{0, 0, 0.375, 1}, true
	case 0x258C:
		return fracRect{0, 0, 0.5, 1}, true // ▌
	case 0x258D:
		return fracRect{0, 0, 0.625, 1}, true
	case 0x258E:
		return fracRect{0, 0, 0.75, 1}, true
	case 0x258F:
		return fracRect{0, 0, 0.875, 1}, true
	case 0x2590:
		return fracRect{0.5, 0, 1, 1}, true // ▐
	case 0x2594:
		return fracRect{0, 0, 1, 0.125}, true // ▔
	case 0x2595:
		return fracRect{0.875, 0, 1, 1}, true // ▕
	case 0x2596:
		return fracRect{0, 0.5, 0.5, 1}, true // ▖
	case 0x2597:
		return fracRect{0.5, 0.5, 1, 1}, true // ▗
	case 0x2598:
		return fracRect{0, 0, 0.5, 0.5}, true // ▘
	case 0x259D:
		return fracRect{0.5, 0, 1, 0.5}, true // ▝
	default:
		return fracRect{}, false
	}
}

// BlockRect is blockSingleRect with plain float32 fractions (0..1 within
// the cell) for pkg/render to consume without depending on font's
// unexported fracRect type.
func BlockRect(r rune) (x0, y0, x1, y1 float32, ok bool) {
	fr, ok := blockSingleRect(r)
	if !ok {
		return 0, 0, 0, 0, false
	}
	return float32(fr.x0), float32(fr.y0), float32(fr.x1), float32(fr.y1), true
}

// shadePred is a fixed-pattern 25/50/75% fill (ordered 4x4 dither).
func shadePred(level float64) inside {
	return func(x, y float64) bool {
		return bayerLevel(int(x), int(y)) < level
	}
}

// bayerLevel returns the 4x4 Bayer threshold for texel (ix, iy) in [0,1),
// handling negative (gutter) coordinates.
func bayerLevel(ix, iy int) float64 {
	ix = ((ix % 4) + 4) % 4
	iy = ((iy % 4) + 4) % 4
	m := [16]float64{0, 8, 2, 10, 12, 4, 14, 6, 3, 11, 1, 9, 15, 7, 13, 5}
	return (m[iy*4+ix] + 0.5) / 16.0
}
