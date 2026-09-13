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

//go:embed assets/FiraCode-Bold.ttf
var defaultBoldFontTTF []byte

// DefaultFontBytes is the vendored font. Currently FiraCode (Nerd Font
// Propo, Medium) — matching the font this was being A/B compared against
// in Ghostty, so any remaining rendering difference isn't confounded by
// comparing two different typefaces. Nothing in this package is specific
// to it; swapping fonts is a one-line change at the call site.
func DefaultFontBytes() []byte { return defaultFontTTF }

// DefaultBoldFontBytes is the vendored font's real Bold cut (FiraCode
// Nerd Font Propo, Bold) — used whenever no system font.family is
// configured, so bold text gets a genuine heavier weight rather than
// just a brightness nudge. There is no italic cut of this family to
// bundle alongside it (see ResolveStyle's doc comment).
func DefaultBoldFontBytes() []byte { return defaultBoldFontTTF }

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
// gets smoother results than native-resolution hinting alone. scale is
// that same raster-to-display ratio (cfg.Atlas.Scale); it sizes the
// gutter around generated sprites (see font.SetOvershoot) so mipmap
// minification doesn't bleed a gap between adjacent cells. gamma
// reshapes the coverage curve (>1 sharpens edges, <1 softens) to taste.
// lineHeight scales the font's own line height into the final cell
// height (1 leaves it unchanged); the extra space splits evenly above
// and below so glyphs stay vertically centered rather than crowding the
// top of a taller cell. Every generated sprite (box drawing, block
// elements, powerline) is drawn to fill that same final cellH, so box
// borders and solid blocks still tile seamlessly across rows whatever
// lineHeight is set to — see spriteGlyph and its callees, which only
// ever work in cellW x cellH fractions, never the font's own metrics. A
// rune the font has no glyph for is left blank rather than failing the
// build, since fonts vary in coverage — except a rune fallbackBytes (the
// bundled Nerd Font, see DefaultFontBytes) has a real glyph for, which is
// rasterized from fallbackBytes instead. Without this, a configured
// font.family that has no Nerd Font icon patch at all — or a different
// one than the bundled font's — would silently lose whatever icons a
// shell prompt relies on just because the user picked a different
// typeface for text; fallbackBytes may be nil to skip this (EnumerateRunes
// on it, elsewhere, already is what the icon set is).
//
// maxTextureSize rejects a packed atlas that would come out larger than
// the GPU will actually accept (see render.MaxTextureSize) — the bundled
// Nerd Font alone enumerates to ~12,000 glyphs, and at a high enough
// atlas.scale the packed texture crosses GL_MAX_TEXTURE_SIZE (commonly
// 16384) well before atlas.scale itself looks like an unreasonable
// number to a user turning it up. Left unchecked, glTexImage2D silently
// fails on upload and every glyph then samples as blank — text just
// disappears, with nothing in this process's own control flow ever
// seeing an error. maxTextureSize <= 0 disables the check (tests that
// don't have a GL context to size against).
func Build(fontBytes []byte, runes []rune, pixelHeight int, gamma float64, scale int, lineHeight float64, fallbackBytes []byte, maxTextureSize int) (*Atlas, error) {
	SetOvershoot(scale)
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
	cellH, ascender = applyLineHeight(cellH, ascender, lineHeight)

	// Guarantee every generated sprite (box drawing, block elements,
	// geometric powerline) is in the atlas even if the loaded font lacks the
	// codepoint — these must never render as blank.
	have := make(map[rune]bool, len(runes))
	for _, r := range runes {
		have[r] = true
	}
	for _, r := range spriteRunes() {
		if !have[r] {
			runes = append(runes, r)
			have[r] = true
		}
	}
	// Symbol fallbacks (checks, crosses, arrows) are added only when the font
	// genuinely lacks the codepoint — a font that carries a real glyph for
	// them keeps it (see sprites_symbols.go).
	for _, r := range symbolRunes {
		if !have[r] {
			runes = append(runes, r)
			have[r] = true
		}
	}

	// Any codepoint fontBytes lacks but fallbackBytes has — icon ranges
	// especially — is rasterized from fallbackBytes instead of left
	// blank. fromFallback records which, so the raster loop below knows
	// which face to actually pull the bitmap from.
	var fallbackFace *ftFace
	fromFallback := make(map[rune]bool)
	if fallbackBytes != nil {
		fallbackFace, err = lib.newMemoryFace(fallbackBytes)
		if err != nil {
			return nil, fmt.Errorf("load fallback font: %w", err)
		}
		defer fallbackFace.free()
		if err := fallbackFace.setPixelSize(pixelHeight); err != nil {
			return nil, err
		}
		for _, r := range fallbackFace.enumerateRunes() {
			if !have[r] {
				runes = append(runes, r)
				have[r] = true
				fromFallback[r] = true
			}
		}
	}

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
	glyphPadding := over + 2
	packedW, packedH := cellW+2*glyphPadding, cellH+2*glyphPadding
	texW, texH := cols*packedW, rows*packedH
	if maxTextureSize > 0 && (texW > maxTextureSize || texH > maxTextureSize) {
		return nil, fmt.Errorf("atlas texture %dx%d exceeds this GPU's max texture size (%d) — lower atlas.scale or font.size",
			texW, texH, maxTextureSize)
	}
	atlas := &Atlas{
		Image:      image.NewAlpha(image.Rect(0, 0, cols*packedW, rows*packedH)),
		CellWidth:  cellW,
		CellHeight: cellH,
		Glyphs:     make(map[rune]Glyph, len(runes)),
	}

	for i, r := range runes {
		gx := (i%cols)*packedW + glyphPadding
		gy := (i/cols)*packedH + glyphPadding
		if !spriteGlyph(r, atlas.Image, gx, gy, cellW, cellH) {
			src := face
			if fromFallback[r] {
				src = fallbackFace
			}
			blitGlyph(src, r, atlas.Image, gx, gy, cellW, cellH, ascender, gamma)
		}
		atlas.Glyphs[r] = Glyph{X: gx, Y: gy, W: cellW, H: cellH}
	}
	return atlas, nil
}

// applyLineHeight scales cellH by lineHeight (1 is a no-op; <= 0 is
// treated as 1, since a zero or negative cell height would break every
// downstream size computation). The added or removed space splits evenly
// above and below the original line box, so ascender shifts by half of
// it — keeping glyphs (and the sprites drawn relative to ascender-free
// cellW x cellH bounds) centered in the new cell instead of pinned to
// its top.
func applyLineHeight(cellH, ascender int, lineHeight float64) (newCellH, newAscender int) {
	if lineHeight <= 0 {
		lineHeight = 1
	}
	newCellH = max(1, int(math.Round(float64(cellH)*lineHeight)))
	newAscender = ascender + (newCellH-cellH)/2
	return newCellH, newAscender
}

// FaceBytes is the raw font file bytes for the four style variants a cell
// can be drawn in. Regular and Bold are always populated (see
// cmd/tubeless's loadFontFaces — Bold always resolves to something real,
// falling back to DefaultBoldFontBytes). Italic/BoldItalic are nil when no
// real italic/bold-italic face was found for the configured family — the
// renderer falls back to a synthetic slant of Regular/Bold in that case
// rather than these being populated with something else.
type FaceBytes struct {
	Regular, Bold, Italic, BoldItalic []byte
}

// Faces is the built-atlas counterpart of FaceBytes: one Atlas per style
// that was actually available. Italic/BoldItalic are nil exactly when the
// corresponding FaceBytes field was nil.
type Faces struct {
	Regular, Bold, Italic, BoldItalic *Atlas
}

// BuildFaces runs Build once per non-nil FaceBytes entry, at the same
// pixelHeight/gamma/scale/lineHeight for all of them so their cell grids
// line up. Regular must be non-nil; Bold is expected to be non-nil too
// (callers always have a real bold face, bundled or resolved) but isn't
// required to be. Every style falls back to the bundled Nerd Font's own
// glyphs (DefaultFontBytes/DefaultBoldFontBytes) for any codepoint it's
// missing, so the icon set available doesn't depend on which family
// config.Font.Family names — see Build's fallbackBytes. maxTextureSize is
// forwarded to every Build call — see Build's own doc comment.
func BuildFaces(bytes FaceBytes, pixelHeight int, gamma float64, scale int, lineHeight float64, maxTextureSize int) (*Faces, error) {
	build := func(name string, b, fallback []byte) (*Atlas, error) {
		if b == nil {
			return nil, nil
		}
		runes, err := EnumerateRunes(b)
		if err != nil {
			return nil, fmt.Errorf("enumerate %s glyphs: %w", name, err)
		}
		atlas, err := Build(b, runes, pixelHeight, gamma, scale, lineHeight, fallback, maxTextureSize)
		if err != nil {
			return nil, fmt.Errorf("build %s atlas: %w", name, err)
		}
		return atlas, nil
	}
	regular, err := build("regular", bytes.Regular, DefaultFontBytes())
	if err != nil {
		return nil, err
	}
	bold, err := build("bold", bytes.Bold, DefaultBoldFontBytes())
	if err != nil {
		return nil, err
	}
	italic, err := build("italic", bytes.Italic, DefaultFontBytes())
	if err != nil {
		return nil, err
	}
	boldItalic, err := build("bold italic", bytes.BoldItalic, DefaultBoldFontBytes())
	if err != nil {
		return nil, err
	}
	return &Faces{Regular: regular, Bold: bold, Italic: italic, BoldItalic: boldItalic}, nil
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
// and centered instead. isPrivateUseRune(r) — icon codepoints — get this
// uniform (aspect-preserving) treatment, since squashing a pictogram
// non-uniformly is obviously wrong.
//
// Ordinary text takes a different fit for the same underlying problem in
// a different shape: an italic cut's slant routinely widens a letter's
// rendered bitmap past cellW (a real 'f' in a Nerd Font monospace italic
// can render 25%+ wider than its upright counterpart, purely from the
// lean) while its height stays well within cellH — this is a width-only
// overflow, never a genuine 2D size problem the way an oversized icon is.
// Two things were tried and both read worse than the fix below: a
// uniform shrink (this same fitScale, applied to all runes) shrank the
// letter's height along with its width, so just the couple of wide
// letters in any given word looked visibly smaller than their neighbors;
// leaving the bitmap alone and letting the per-pixel clip in the copy
// loop below silently drop the overflow cropped chunks out of those same
// letters, which is worse. Squeezing width alone — never touching
// height — keeps every letter the same height as its neighbors (so nothing
// reads as "smaller") and keeps the whole glyph shape intact (so nothing
// reads as "missing"); the trade is a slightly narrower letter, which is
// far less perceptible than either of those.
func blitGlyph(face *ftFace, r rune, dst *image.Alpha, gx, gy, cellW, cellH, ascender int, gamma float64) {
	pix, w, h, left, top, ok := face.glyphBitmap(r)
	if !ok || w == 0 || h == 0 {
		return
	}
	switch {
	case isPrivateUseRune(r):
		if scale := fitScale(w, h, cellW, cellH); scale < 1 {
			pix, w, h = scaleCoverage(pix, w, h, scale)
			left = int(float64(left) * scale)
			top = int(float64(top) * scale)
		}
	case w > cellW || h > cellH:
		// Independent per-axis clamp, not fitScale's uniform one — a
		// width-only overflow (the common italic case) stays exactly
		// full height, only narrower. The rare font where a text glyph
		// overflows vertically instead gets the same treatment on that
		// axis, still without touching the other.
		ow, oh := min(w, cellW), min(h, cellH)
		wScale, hScale := float64(ow)/float64(w), float64(oh)/float64(h)
		pix, w, h = resizeCoverage(pix, w, h, ow, oh)
		left, top = int(float64(left)*wScale), int(float64(top)*hScale)
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

// isPrivateUseRune reports whether r falls in one of the three Unicode
// Private Use Areas — where every Nerd Font icon range lives (Powerline,
// Devicons, Font Awesome, Seti-UI, Material Design, etc.), whether
// patched into the configured font directly or pulled from the bundled
// fallback. Ordinary text (Latin, box-drawing, CJK, emoji, ...) never
// lands here, which is what lets blitGlyph apply its shrink-to-fit only
// to icons and leave real letterforms at their natural rasterized size.
func isPrivateUseRune(r rune) bool {
	return (r >= 0xE000 && r <= 0xF8FF) ||
		(r >= 0xF0000 && r <= 0xFFFFD) ||
		(r >= 0x100000 && r <= 0x10FFFD)
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
// scale (< 1) uniformly on both axes — an icon's aspect ratio must stay
// fixed, unlike resizeCoverage's independent-axis general case.
func scaleCoverage(pix []byte, w, h int, scale float64) (out []byte, ow, oh int) {
	return resizeCoverage(pix, w, h, max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale)))
}

// resizeCoverage box-filters a single-channel coverage bitmap from w x h
// down to ow x oh — independently on each axis, so a width-only squeeze
// (ow < w, oh == h) is exactly as valid a call as a uniform shrink —
// area-averaging each output texel's source region. A straight
// nearest/point resample would just re-introduce aliasing on the very
// edges this exists to clean up. Only ever shrinks in practice (callers
// never grow a dimension), though nothing here assumes that.
func resizeCoverage(pix []byte, w, h, ow, oh int) (out []byte, outW, outH int) {
	ow, oh = max(1, ow), max(1, oh)
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
