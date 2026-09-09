package font

// This file's glyph rasterization is a cgo binding to FreeType, which
// must be present as a system dev package at build time — freetype2
// headers plus pkg-config itself. On Linux that's typically already
// installed or a single distro package away (e.g.
// libfreetype-dev/freetype-devel); on macOS it's not present by default:
// `brew install freetype pkg-config` before building. This is a
// build-time prerequisite only — the compiled binary doesn't need
// FreeType installed at runtime; it links it in.

/*
#cgo pkg-config: freetype2
#include <ft2build.h>
#include FT_FREETYPE_H

// FT_LOAD_TARGET_LIGHT expands through a function-like macro cgo can't
// translate directly; wrapping the combined flags in a tiny static
// function sidesteps that entirely.
static int ft_load_flags(void) {
	return FT_LOAD_RENDER | FT_LOAD_TARGET_LIGHT;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// ftLibrary owns a FreeType library handle. FreeType is not safe for
// concurrent use across goroutines without external locking; we only ever
// use it during atlas Build(), never from the render loop, so no locking
// is needed here.
type ftLibrary struct {
	lib C.FT_Library
}

func newFTLibrary() (*ftLibrary, error) {
	var lib C.FT_Library
	if C.FT_Init_FreeType(&lib) != 0 {
		return nil, fmt.Errorf("FT_Init_FreeType failed")
	}
	return &ftLibrary{lib: lib}, nil
}

func (l *ftLibrary) free() {
	C.FT_Done_FreeType(l.lib)
}

// ftFace wraps a loaded font face. cdata holds the C-owned copy of the
// font bytes FreeType keeps a pointer into for the face's lifetime — Go's
// GC could move or free a Go-owned slice out from under FreeType, so we
// copy into C memory instead and free it ourselves in free().
type ftFace struct {
	face  C.FT_Face
	cdata unsafe.Pointer
}

func (l *ftLibrary) newMemoryFace(data []byte) (*ftFace, error) {
	cdata := C.CBytes(data)
	var face C.FT_Face
	ret := C.FT_New_Memory_Face(l.lib, (*C.FT_Byte)(cdata), C.FT_Long(len(data)), 0, &face)
	if ret != 0 {
		C.free(cdata)
		return nil, fmt.Errorf("FT_New_Memory_Face failed: code %d", ret)
	}
	return &ftFace{face: face, cdata: cdata}, nil
}

func (f *ftFace) free() {
	C.FT_Done_Face(f.face)
	C.free(f.cdata)
}

func (f *ftFace) setPixelSize(px int) error {
	if C.FT_Set_Pixel_Sizes(f.face, 0, C.FT_UInt(px)) != 0 {
		return fmt.Errorf("FT_Set_Pixel_Sizes(%d) failed", px)
	}
	return nil
}

// advancePixels returns the hinted advance width for r, in whole pixels,
// or 0 if the font has no glyph for r.
func (f *ftFace) advancePixels(r rune) int {
	idx := C.FT_Get_Char_Index(f.face, C.FT_ULong(r))
	if idx == 0 {
		return 0
	}
	if C.FT_Load_Glyph(f.face, idx, C.FT_Int32(C.ft_load_flags())) != 0 {
		return 0
	}
	return int(f.face.glyph.advance.x >> 6)
}

// lineMetrics returns the face's recommended line height and ascender, in
// whole pixels, at the currently set pixel size.
func (f *ftFace) lineMetrics() (height, ascender int) {
	m := f.face.size.metrics
	return int(m.height >> 6), int(m.ascender >> 6)
}

// enumerateRunes walks the face's charmap directly (FT_Get_First_Char /
// FT_Get_Next_Char) rather than guessing which Unicode ranges the font
// covers. This is what makes a Nerd Font's icon glyphs (Devicons, Font
// Awesome, Powerline, etc. — scattered across Private Use Area ranges no
// hardcoded rune list would reliably match) show up, and it adapts
// automatically to whatever font is actually loaded via --font.
func (f *ftFace) enumerateRunes() []rune {
	var runes []rune
	var idx C.FT_UInt
	code := C.FT_Get_First_Char(f.face, &idx)
	for idx != 0 {
		runes = append(runes, rune(code))
		code = C.FT_Get_Next_Char(f.face, code, &idx)
	}
	return runes
}

// glyphBitmap loads and renders r, returning its coverage bitmap and the
// pen-relative placement (bitmapLeft/bitmapTop, FreeType's usual bearing
// convention) to draw it at. ok is false if the font has no glyph for r —
// callers should leave that cell blank rather than fail the whole atlas,
// since not every font covers every rune we ask for.
func (f *ftFace) glyphBitmap(r rune) (pix []byte, w, h, bitmapLeft, bitmapTop int, ok bool) {
	idx := C.FT_Get_Char_Index(f.face, C.FT_ULong(r))
	if idx == 0 {
		return nil, 0, 0, 0, 0, false
	}
	if C.FT_Load_Glyph(f.face, idx, C.FT_Int32(C.ft_load_flags())) != 0 {
		return nil, 0, 0, 0, 0, false
	}
	slot := f.face.glyph
	bmp := slot.bitmap
	if bmp.width == 0 || bmp.rows == 0 {
		return nil, 0, 0, int(slot.bitmap_left), int(slot.bitmap_top), true
	}
	// FreeType's default 8-bit grayscale pitch is >= width (row-padded);
	// copy row by row rather than assuming pitch == width.
	w, h = int(bmp.width), int(bmp.rows)
	pitch := int(bmp.pitch)
	src := unsafe.Slice((*byte)(unsafe.Pointer(bmp.buffer)), h*pitch)
	pix = make([]byte, w*h)
	for y := 0; y < h; y++ {
		copy(pix[y*w:(y+1)*w], src[y*pitch:y*pitch+w])
	}
	return pix, w, h, int(slot.bitmap_left), int(slot.bitmap_top), true
}
