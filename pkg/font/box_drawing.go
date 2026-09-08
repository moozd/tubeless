package font

import (
	"image"
	"image/color"
)

// boxDrawingSpec says which of the four cell edges a light single-line
// box-drawing character's stroke reaches from the cell's center.
type boxDrawingSpec struct{ up, down, left, right bool }

// boxDrawing covers the light single-line set (U+2500-U+2503, U+250C-
// U+2537 minus the double/heavy/rounded variants) — what tree views,
// table borders, and window borders actually draw with. Rendered
// procedurally instead of through the font (see renderBoxDrawing): a
// font's own box-drawing glyphs are drawn within its normal glyph em-box,
// which routinely doesn't reach the cell's actual edges the way our grid
// lays cells out, leaving visible gaps where a vertical or horizontal
// line crosses from one cell into the next. A generated line that
// deliberately spans center-to-edge always connects seamlessly,
// regardless of what the loaded font would have drawn — the same reason
// Ghostty generates these rather than trusting the font (src/font/sprite
// in their source).
var boxDrawing = map[rune]boxDrawingSpec{
	0x2500: {left: true, right: true},                       // ─
	0x2502: {up: true, down: true},                          // │
	0x250C: {down: true, right: true},                       // ┌
	0x2510: {down: true, left: true},                        // ┐
	0x2514: {up: true, right: true},                         // └
	0x2518: {up: true, left: true},                          // ┘
	0x251C: {up: true, down: true, right: true},             // ├
	0x2524: {up: true, down: true, left: true},              // ┤
	0x252C: {down: true, left: true, right: true},           // ┬
	0x2534: {up: true, left: true, right: true},             // ┴
	0x253C: {up: true, down: true, left: true, right: true}, // ┼
}

// renderBoxDrawing paints spec's line segments into dst's (gx,gy) cell,
// full coverage (255), each segment running from the cell's center to
// the edge(s) spec calls for — so a vertical line in one cell always
// meets a vertical line in the cell below it with no gap, independent of
// font metrics.
func renderBoxDrawing(dst *image.Alpha, gx, gy, cellW, cellH int, spec boxDrawingSpec) {
	thickness := max(1, cellW/8)
	half := thickness / 2
	cx, cy := gx+cellW/2, gy+cellH/2

	// Segments overshoot slightly past the cell edge into the packing
	// gutter (see glyphPadding in atlas.go — 4px, comfortably more than
	// this needs). On-screen, adjacent grid cells rarely land on exact
	// integer pixel boundaries (cell size is a DPI-scaled float), so a
	// line that stopped exactly at the cell edge can leave a hairline
	// sub-pixel gap against the next cell's line at the GPU sampling
	// stage. Overlapping slightly instead of exactly touching absorbs
	// that without visibly thickening the line at display size.
	const overshoot = 2

	fill := func(x0, y0, x1, y1 int) {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				dst.SetAlpha(x, y, color.Alpha{A: 255})
			}
		}
	}

	if spec.up {
		fill(cx-half, gy-overshoot, cx-half+thickness, cy+half)
	}
	if spec.down {
		fill(cx-half, cy-half, cx-half+thickness, gy+cellH+overshoot)
	}
	if spec.left {
		fill(gx-overshoot, cy-half, cx+half, cy-half+thickness)
	}
	if spec.right {
		fill(cx-half, cy-half, gx+cellW+overshoot, cy-half+thickness)
	}
}
