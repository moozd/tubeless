// Package font rasterizes a TTF/OTF font into a texture atlas of fixed-size
// monospace cells for the GL renderer to sample. Rasterization goes through
// real FreeType (via cgo) rather than a hand-rolled rasterizer, so any font
// gets FreeType's autohinter and mature anti-aliasing — the same approach
// Kitty and Ghostty use — instead of needing supersampling to compensate
// for a cruder rasterizer.
package font

import (
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"math"
)

//go:embed assets/FiraCode-Medium.ttf
var defaultFontTTF []byte

// DefaultFontBytes is the vendored font. Currently FiraCode (Nerd Font
// Propo, Medium) — matching the font this was being A/B compared against
// in Ghostty, so any remaining rendering difference isn't confounded by
// comparing two different typefaces. Nothing in this package is specific
// to it; swapping fonts is a one-line change at the call site.
func DefaultFontBytes() []byte { return defaultFontTTF }

// Glyph is one atlas entry: its pixel rect within Atlas.Image.
type Glyph struct {
	X, Y, W, H int
}

// Atlas is a single-channel coverage texture holding every rasterized
// glyph, plus the fixed cell size the renderer should lay the grid out at.
type Atlas struct {
	Image      *image.Alpha
	CellWidth  int
	CellHeight int
	Glyphs     map[rune]Glyph
}

// maxAtlasRunes bounds how many codepoints EnumerateRunes will return, so
// an unusually broad font (e.g. one with real CJK coverage) can't blow the
// atlas texture past typical GL_MAX_TEXTURE_SIZE limits or make startup
// unreasonably slow. Comfortably covers a Latin coding font's own glyphs
// plus a full Nerd Font icon patch (a few thousand codepoints).
const maxAtlasRunes = 16384

// EnumerateRunes returns every codepoint fontBytes has a glyph for, via
// the font's own charmap — see ftFace.enumerateRunes.
func EnumerateRunes(fontBytes []byte) ([]rune, error) {
	lib, err := newFTLibrary()
	if err != nil {
		return nil, fmt.Errorf("init freetype: %w", err)
	}
	defer lib.free()

	face, err := lib.newMemoryFace(fontBytes)
	if err != nil {
		return nil, fmt.Errorf("load font: %w", err)
	}
	defer face.free()

	runes := face.enumerateRunes()
	if len(runes) > maxAtlasRunes {
		runes = runes[:maxAtlasRunes]
	}
	return runes, nil
}

// Build rasterizes runes from fontBytes at pixelHeight into a packed atlas.
// pixelHeight is the atlas's own raster resolution, not necessarily the
// on-screen cell size — a caller after more detail than it will actually
// display at (see cmd/tubeless's atlasScale) rasterizes at a larger
// pixelHeight and lets the GPU minify with mipmaps when drawing, which
// gets smoother results than native-resolution hinting alone. gamma
// reshapes the coverage curve (>1 sharpens edges, <1 softens) to taste. A
// rune the font has no glyph for is left blank rather than failing the
// build, since fonts vary in coverage.
func Build(fontBytes []byte, runes []rune, pixelHeight int, gamma float64) (*Atlas, error) {
	lib, err := newFTLibrary()
	if err != nil {
		return nil, fmt.Errorf("init freetype: %w", err)
	}
	defer lib.free()

	face, err := lib.newMemoryFace(fontBytes)
	if err != nil {
		return nil, fmt.Errorf("load font: %w", err)
	}
	defer face.free()

	if err := face.setPixelSize(pixelHeight); err != nil {
		return nil, err
	}

	cellW := face.advancePixels('M')
	if cellW == 0 {
		return nil, fmt.Errorf("font has no glyph for 'M' to measure cell width")
	}
	cellH, ascender := face.lineMetrics()

	// Packed roughly square rather than a fixed column count: with
	// EnumerateRunes potentially returning several thousand codepoints,
	// a fixed narrow column count would produce a very tall, thin
	// texture that's more likely to exceed GL_MAX_TEXTURE_SIZE in one
	// dimension even though the total texel count is fine.
	cols := max(1, int(math.Ceil(math.Sqrt(float64(len(runes))))))
	rows := (len(runes) + cols - 1) / cols

	// Packed cells are laid out glyphPadding pixels apart (blank gutter,
	// always alpha 0), not edge-to-edge — mipmap generation (see
	// uploadAtlas) box-filters neighboring texels together at each level,
	// and with cells packed tight that blends adjacent glyphs into each
	// other's edges once the GPU samples a deeper mip. Icon glyphs (see
	// blitGlyph's fitScale) are hit hardest since they're scaled to fill
	// their cell rather than sitting inset like most text glyphs do. The
	// gutter is packing-only: Glyph.W/H below still describe just the
	// drawable cellW x cellH, so on-screen cell size is unaffected.
	const glyphPadding = 4
	packedW, packedH := cellW+2*glyphPadding, cellH+2*glyphPadding
	atlas := &Atlas{
		Image:      image.NewAlpha(image.Rect(0, 0, cols*packedW, rows*packedH)),
		CellWidth:  cellW,
		CellHeight: cellH,
		Glyphs:     make(map[rune]Glyph, len(runes)),
	}

	for i, r := range runes {
		gx := (i%cols)*packedW + glyphPadding
		gy := (i/cols)*packedH + glyphPadding
		if spec, ok := boxDrawing[r]; ok {
			renderBoxDrawing(atlas.Image, gx, gy, cellW, cellH, spec)
		} else {
			blitGlyph(face, r, atlas.Image, gx, gy, cellW, cellH, ascender, gamma)
		}
		atlas.Glyphs[r] = Glyph{X: gx, Y: gy, W: cellW, H: cellH}
	}
	return atlas, nil
}

// blitGlyph rasterizes r and copies its coverage into dst's (gx,gy) cell,
// baseline-aligned using ascender. Most glyphs (any well-behaved Latin
// glyph in a monospace font) already fit within cellW x cellH. Icon
// glyphs — Nerd Font Private Use Area icons especially — routinely don't:
// their design width/height comes from the icon font they were patched
// in from, not from the base font's own metrics. Hard-clipping those at
// the cell edge is what reads as "half an icon"; scaling them down to
// fit (Ghostty does the equivalent with a curated per-icon constraint
// table generated from the Nerd Font patcher's own stretch rules — this
// is a simpler, general version of the same idea: scale down whatever
// doesn't fit, uniformly, only when it doesn't) keeps every glyph intact
// and centered instead.
func blitGlyph(face *ftFace, r rune, dst *image.Alpha, gx, gy, cellW, cellH, ascender int, gamma float64) {
	pix, w, h, left, top, ok := face.glyphBitmap(r)
	if !ok || w == 0 || h == 0 {
		return
	}
	if scale := fitScale(w, h, cellW, cellH); scale < 1 {
		pix, w, h = scaleCoverage(pix, w, h, scale)
		left = int(float64(left) * scale)
		top = int(float64(top) * scale)
	}
	originX, originY := gx+left, gy+ascender-top
	// fitScale only guarantees the bitmap's own w x h is small enough to
	// fit the cell — it says nothing about where the font's bearing
	// metrics place it. An icon glyph with unusual bearing (patched in
	// from a different font, at different design coordinates than the
	// base font's own glyphs) can still land partly outside the cell
	// even though it's small enough to fit, reading as a cropped icon.
	// Clamping the origin — never scaling, since w/h are already known
	// to fit — keeps it fully on-screen.
	originX = clampOrigin(originX, w, gx, cellW)
	originY = clampOrigin(originY, h, gy, cellH)
	invGamma := 1 / gamma
	for y := range h {
		dy := originY + y
		if dy < gy || dy >= gy+cellH {
			continue
		}
		for x := range w {
			dx := originX + x
			if dx < gx || dx >= gx+cellW {
				continue
			}
			coverage := pix[y*w+x]
			corrected := math.Pow(float64(coverage)/255, invGamma) * 255
			dst.SetAlpha(dx, dy, color.Alpha{A: clampByte(corrected)})
		}
	}
}

// clampOrigin shifts a size-d span starting at origin so it lands fully
// within [cellStart, cellStart+cellSize) — d is assumed <= cellSize
// already (fitScale's job), so a shift that satisfies both bounds always
// exists.
func clampOrigin(origin, d, cellStart, cellSize int) int {
	if origin < cellStart {
		return cellStart
	}
	if origin+d > cellStart+cellSize {
		return cellStart + cellSize - d
	}
	return origin
}

// fitScale returns the uniform scale factor (<= 1) needed to bring a
// w x h glyph within cellW x cellH, or 1 if it already fits.
func fitScale(w, h, cellW, cellH int) float64 {
	scale := 1.0
	if w > cellW {
		scale = float64(cellW) / float64(w)
	}
	if hScale := float64(cellH) / float64(h); h > cellH && hScale < scale {
		scale = hScale
	}
	return scale
}

// scaleCoverage box-filters a single-channel coverage bitmap down by
// scale (< 1), area-averaging each output texel's source region — a
// straight nearest/point resample would just re-introduce aliasing on
// the very edges this exists to clean up.
func scaleCoverage(pix []byte, w, h int, scale float64) (out []byte, ow, oh int) {
	ow, oh = max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))
	out = make([]byte, ow*oh)
	for oy := range oh {
		sy0, sy1 := srcRange(oy, oh, h)
		for ox := range ow {
			sx0, sx1 := srcRange(ox, ow, w)
			sum, count := 0, 0
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					sum += int(pix[sy*w+sx])
					count++
				}
			}
			if count > 0 {
				out[oy*ow+ox] = byte(sum / count)
			}
		}
	}
	return out, ow, oh
}

// srcRange maps output index i (of n) back to the [start,end) span of
// source pixels (of total srcN) it should average, guaranteeing a
// non-empty span even when n > srcN.
func srcRange(i, n, srcN int) (start, end int) {
	start = i * srcN / n
	end = (i + 1) * srcN / n
	if end <= start {
		end = start + 1
	}
	if end > srcN {
		end = srcN
	}
	return start, end
}

func clampByte(v float64) uint8 {
	if v > 255 {
		return 255
	}
	if v < 0 {
		return 0
	}
	return uint8(v)
}
