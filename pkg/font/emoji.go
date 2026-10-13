package font

// Color emoji come from the system's emoji font (Apple Color Emoji on
// macOS, Noto Color Emoji on Linux), not the glyph atlas: those fonts
// are bitmap-only (sbix/CBDT strikes of BGRA PNGs), hold thousands of
// glyphs plus ZWJ/flag/skin-tone sequences, and Apple's file alone is
// ~190 MB. So EmojiFace opens the file directly (FreeType and HarfBuzz
// both map it instead of copying it into memory) and rasterizes one
// cluster at a time, on demand — the renderer caches each result.

/*
#cgo pkg-config: freetype2 harfbuzz
#include <stdlib.h>
#include <ft2build.h>
#include FT_FREETYPE_H
#include <hb.h>

static int ft_color_load_flags(void) {
	return FT_LOAD_COLOR | FT_LOAD_RENDER;
}
*/
import "C"

import (
	"fmt"
	"image"
	"os"
	"os/exec"
	"strings"
	"unsafe"
)

// macEmojiFontPath is where macOS ships Apple Color Emoji.
const macEmojiFontPath = "/System/Library/Fonts/Apple Color Emoji.ttc"

// EmojiFace rasterizes emoji grapheme clusters from the system's color
// emoji font. Like the atlas build, it is not safe for concurrent use.
type EmojiFace struct {
	lib    C.FT_Library
	face   C.FT_Face
	blob   *C.hb_blob_t
	hbFace *C.hb_face_t
	hbFont *C.hb_font_t
	buffer *C.hb_buffer_t
}

// FindEmojiFont returns the path of the system's color emoji font.
func FindEmojiFont() (string, error) {
	if _, err := os.Stat(macEmojiFontPath); err == nil {
		return macEmojiFontPath, nil
	}
	out, err := exec.Command("fc-match", "-f", "%{file}", "emoji:color=true").Output()
	if err != nil {
		return "", fmt.Errorf("find emoji font with fc-match: %w", err)
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", fmt.Errorf("fc-match found no color emoji font")
	}
	return path, nil
}

// OpenEmojiFace opens the emoji font at path and selects the bitmap
// strike nearest to pixelHeight (the smallest one at least that tall,
// else the largest), so the renderer only ever scales a strike down.
func OpenEmojiFace(path string, pixelHeight int) (*EmojiFace, error) {
	e := &EmojiFace{}
	if C.FT_Init_FreeType(&e.lib) != 0 {
		return nil, fmt.Errorf("FT_Init_FreeType failed")
	}
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	if ret := C.FT_New_Face(e.lib, cpath, 0, &e.face); ret != 0 {
		C.FT_Done_FreeType(e.lib)
		return nil, fmt.Errorf("open emoji font %s: FT_New_Face code %d", path, ret)
	}
	if err := e.selectStrike(pixelHeight); err != nil {
		e.Close()
		return nil, fmt.Errorf("open emoji font %s: %w", path, err)
	}
	e.blob = C.hb_blob_create_from_file(cpath)
	e.hbFace = C.hb_face_create(e.blob, 0)
	e.hbFont = C.hb_font_create(e.hbFace)
	e.buffer = C.hb_buffer_create()
	return e, nil
}

// selectStrike picks the bitmap strike for pixelHeight. A scalable color
// font has no strikes; it gets a plain pixel size instead.
func (e *EmojiFace) selectStrike(pixelHeight int) error {
	n := int(e.face.num_fixed_sizes)
	if n == 0 {
		if C.FT_Set_Pixel_Sizes(e.face, 0, C.FT_UInt(pixelHeight)) != 0 {
			return fmt.Errorf("FT_Set_Pixel_Sizes(%d) failed", pixelHeight)
		}
		return nil
	}
	sizes := unsafe.Slice(e.face.available_sizes, n)
	best := 0
	for i, s := range sizes {
		if isBetterStrike(int(s.height), int(sizes[best].height), pixelHeight) {
			best = i
		}
	}
	if C.FT_Select_Size(e.face, C.FT_Int(best)) != 0 {
		return fmt.Errorf("FT_Select_Size(%d) failed", best)
	}
	return nil
}

// isBetterStrike reports whether a strike of height h beats the current
// pick cur for target: any strike at least target tall beats one that is
// shorter, the shortest such strike wins, else the tallest one does.
func isBetterStrike(h, cur, target int) bool {
	if (h >= target) != (cur >= target) {
		return h >= target
	}
	if h >= target {
		return h < cur
	}
	return h > cur
}

// Close frees the face.
func (e *EmojiFace) Close() {
	if e.buffer != nil {
		C.hb_buffer_destroy(e.buffer)
		C.hb_font_destroy(e.hbFont)
		C.hb_face_destroy(e.hbFace)
		C.hb_blob_destroy(e.blob)
	}
	C.FT_Done_Face(e.face)
	C.FT_Done_FreeType(e.lib)
}

// Render rasterizes cluster (one grapheme cluster, e.g. "🚀", "❤\uFE0F"
// or a ZWJ family) into a premultiplied RGBA image at the selected
// strike's size. ok is false when the font has no glyph for it.
func (e *EmojiFace) Render(cluster string) (img *image.RGBA, ok bool) {
	for _, gid := range e.clusterGlyphs(cluster) {
		if img, ok := e.renderGlyph(gid); ok {
			return img, true
		}
	}
	return nil, false
}

// renderGlyph loads gid as a color bitmap. Some Apple Color Emoji
// strikes (the ZWJ family glyphs) use Apple's own compressed "emjc"
// format, which FreeType loads as an empty bitmap: that is not ok.
func (e *EmojiFace) renderGlyph(gid uint32) (*image.RGBA, bool) {
	if gid == 0 {
		return nil, false
	}
	if C.FT_Load_Glyph(e.face, C.FT_UInt(gid), C.FT_Int32(C.ft_color_load_flags())) != 0 {
		return nil, false
	}
	bmp := e.face.glyph.bitmap
	if bmp.pixel_mode != C.FT_PIXEL_MODE_BGRA || bmp.width == 0 || bmp.rows == 0 {
		return nil, false
	}
	return bgraToRGBA(bmp), true
}

// clusterGlyphs returns the glyphs to try for cluster, best first: the
// one glyph a sequence shapes to, then its first emoji on its own.
// Variation selectors are dropped first: they only pick emoji
// presentation, which this face always gives.
func (e *EmojiFace) clusterGlyphs(cluster string) []uint32 {
	runes := []rune(strings.NewReplacer("\uFE0F", "", "\uFE0E", "").Replace(cluster))
	if len(runes) == 0 {
		return nil
	}
	first := uint32(C.FT_Get_Char_Index(e.face, C.FT_ULong(runes[0])))
	if len(runes) == 1 {
		return []uint32{first}
	}
	if gid, ok := e.shapeToOne(runes); ok {
		return []uint32{gid, first}
	}
	return []uint32{first}
}

// shapeToOne shapes runes with HarfBuzz and reports the glyph when they
// collapse to exactly one. Apple Color Emoji merges sequences in its
// AAT morx table rather than GSUB; HarfBuzz runs both.
func (e *EmojiFace) shapeToOne(runes []rune) (uint32, bool) {
	C.hb_buffer_reset(e.buffer)
	cps := make([]C.uint32_t, len(runes))
	for i, r := range runes {
		cps[i] = C.uint32_t(r)
	}
	C.hb_buffer_add_utf32(e.buffer, &cps[0], C.int(len(cps)), 0, C.int(len(cps)))
	C.hb_buffer_guess_segment_properties(e.buffer)
	C.hb_shape(e.hbFont, e.buffer, nil, 0)
	if C.hb_buffer_get_length(e.buffer) != 1 {
		return 0, false
	}
	info := *C.hb_buffer_get_glyph_infos(e.buffer, nil)
	return uint32(info.codepoint), info.codepoint != 0
}

// bgraToRGBA copies FreeType's premultiplied BGRA bitmap into Go's
// premultiplied RGBA layout, row by row since pitch may pad each row.
func bgraToRGBA(bmp C.FT_Bitmap) *image.RGBA {
	w, h, pitch := int(bmp.width), int(bmp.rows), int(bmp.pitch)
	src := unsafe.Slice((*byte)(unsafe.Pointer(bmp.buffer)), h*pitch)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		row := src[y*pitch : y*pitch+4*w]
		dst := img.Pix[y*img.Stride : y*img.Stride+4*w]
		for x := range w {
			b, g, r, a := row[4*x], row[4*x+1], row[4*x+2], row[4*x+3]
			dst[4*x], dst[4*x+1], dst[4*x+2], dst[4*x+3] = r, g, b, a
		}
	}
	return img
}
