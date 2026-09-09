package font

import (
	"image"
	"image/color"
	"math"
)

// This file generates the "graphics" codepoints a terminal should never trust
// the font for — box drawing (U+2500-U+257F) and block elements
// (U+2580-U+259F). A font draws these inside its normal glyph em-box, which
// routinely doesn't reach the cell's actual edges the way the grid lays cells
// out, leaving visible gaps where a line or block crosses from one cell into
// the next — or glyphs that read as broken/empty. Generating them from pure
// geometry (the same reason Ghostty does, src/font/sprite) makes every piece
// reach the edges and connect seamlessly. Powerline triangle/chevron glyphs
// (geometric, straight-edged PUA icons) are included here too; the curved
// ones are left to the font, which already has good coverage.

// spriteGlyph renders rune r into the atlas at cell (gx, gy) of size
// cellW x cellH, returning whether it owns r (true) or the caller should fall
// back to FreeType (false).
func spriteGlyph(r rune, img *image.Alpha, gx, gy, cellW, cellH int) bool {
	switch {
	case r >= 0x2500 && r <= 0x257F:
		drawBoxSprite(r, img, gx, gy, cellW, cellH)
	case r >= 0x2580 && r <= 0x259F:
		drawBlockSprite(r, img, gx, gy, cellW, cellH)
	case isPowerlineSprite(r):
		drawPowerlineSprite(r, img, gx, gy, cellW, cellH)
	case isSymbolSprite(r):
		drawSymbolSprite(r, img, gx, gy, cellW, cellH)
	default:
		return false
	}
	return true
}

// IsShapeRune reports whether r is a "structural" glyph the renderer draws
// on its bloomable line-art layer instead of the sharp text layer: box
// drawing (U+2500-U+257F), the generated straight-edged powerline pieces,
// and the block-element codepoints (U+2580-U+259F) that aren't a single
// rectangle — the shaded fills (░▒▓) and multi-rect quadrant unions (▙▚▛▜
// ▞▟). Single-rect block elements (█▀▄▌▐ etc.) are intercepted earlier, in
// pkg/render's CellPass.BuildInstances, and drawn procedurally with true
// rounded corners instead (see font.BlockRect). Everything else — letters,
// symbols, icons — is text and stays crisp.
func IsShapeRune(r rune) bool {
	switch {
	case r >= 0x2500 && r <= 0x259F:
		return true
	case isPowerlineSprite(r):
		return true
	default:
		return false
	}
}

// spriteRunes returns the codepoints this package generates, so atlas.Build
// can make sure they're present even if the loaded font lacks them.
func spriteRunes() []rune {
	var out []rune
	for r := 0x2500; r <= 0x259F; r++ {
		out = append(out, rune(r))
	}
	for _, r := range powerlineRunes {
		out = append(out, r)
	}
	return out
}

// lineThickness is a light box line's width in atlas pixels, scaled to the
// cell so a light line stays ~1 display pixel regardless of atlasScale (the
// atlas is rasterized larger than the on-screen cell and mipmap-minified). A
// heavy line is 2x; a double line is a light pair with a light gap between.
func lineThickness(cellW, cellH int) int {
	base := int(math.Round(float64(min(cellW, cellH)) * 0.083))
	return max(2, base)
}

// ---------- raster kernel ----------

// over is how far generated shapes may reach past a cell edge into the atlas
// gutter, so adjacent cells join without a hairline once GPU mipmapping
// box-filters across a cell boundary. It must scale with the atlas's
// raster-to-display ratio (see SetOvershoot) — the more mip levels between
// the rasterized atlas and the displayed cell, the further each level's box
// filter reaches back into the source texture. Must stay < glyphPadding
// (atlas.go).
var over = 2

// SetOvershoot grows the sprite gutter overshoot to match cfg.Atlas.Scale
// (raster pixels per on-screen pixel). Build calls this once before
// rasterizing sprites, so a larger atlas scale — and the deeper mip chain
// it implies — doesn't bleed into the blank part of the gutter.
func SetOvershoot(scale int) {
	if scale > over {
		over = scale
	}
}

const supersample = 4

type rect struct{ x0, y0, x1, y1 float64 }

// inside reports whether a local cell coordinate (see raster) is covered.
type inside func(x, y float64) bool

// raster paints every texel of the cell plus the over-px gutter margin with
// coverage computed by supersampling inside. Geometry is in local
// coordinates: (0,0) is the cell's top-left, and the sample region spans
// [-over, cellW+over) x [-over, cellH+over), so edge-reaching shapes are
// specified slightly past the edge.
func raster(img *image.Alpha, gx, gy, cellW, cellH int, pred inside) {
	for oy := -over; oy < cellH+over; oy++ {
		for ox := -over; ox < cellW+over; ox++ {
			count := 0
			for sy := 0; sy < supersample; sy++ {
				y := float64(oy) + (float64(sy)+0.5)/supersample
				for sx := 0; sx < supersample; sx++ {
					x := float64(ox) + (float64(sx)+0.5)/supersample
					if pred(x, y) {
						count++
					}
				}
			}
			img.SetAlpha(gx+ox, gy+oy, color.Alpha{A: uint8(count * 255 / (supersample * supersample))})
		}
	}
}

func (r rect) contains(x, y float64) bool {
	return x >= r.x0 && x < r.x1 && y >= r.y0 && y < r.y1
}

// expand pushes r's sides that already touch a cell boundary (x=0, y=0,
// x=cellW or y=cellH) out by over into the gutter.
func expand(cellW, cellH int, r rect) rect {
	if r.x0 == 0 {
		r.x0 = -float64(over)
	}
	if r.y0 == 0 {
		r.y0 = -float64(over)
	}
	if r.x1 == float64(cellW) {
		r.x1 = float64(cellW) + float64(over)
	}
	if r.y1 == float64(cellH) {
		r.y1 = float64(cellH) + float64(over)
	}
	return r
}

func union(rs []rect) inside {
	return func(x, y float64) bool {
		for _, r := range rs {
			if r.contains(x, y) {
				return true
			}
		}
		return false
	}
}

func distSeg(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return math.Hypot(px-ax, py-ay)
	}
	t := ((px-ax)*dx + (py-ay)*dy) / l2
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(px-(ax+t*dx), py-(ay+t*dy))
}

func stroke(centerlines []segment, half float64) inside {
	return func(x, y float64) bool {
		for _, s := range centerlines {
			if distSeg(x, y, s.ax, s.ay, s.bx, s.by) <= half {
				return true
			}
		}
		return false
	}
}

type segment struct{ ax, ay, bx, by float64 }

func seg(ax, ay, bx, by float64) segment { return segment{ax, ay, bx, by} }

// inTri reports whether p is inside triangle abc (any orientation, convex).
func inTri(x, y float64, a, b, c [2]float64) bool {
	d1 := (x-b[0])*(a[1]-b[1]) - (a[0]-b[0])*(y-b[1])
	d2 := (x-c[0])*(b[1]-c[1]) - (b[0]-c[0])*(y-c[1])
	d3 := (x-a[0])*(c[1]-a[1]) - (c[0]-a[0])*(y-a[1])
	hasNeg := d1 < 0 || d2 < 0 || d3 < 0
	hasPos := d1 > 0 || d2 > 0 || d3 > 0
	return !(hasNeg && hasPos)
}
