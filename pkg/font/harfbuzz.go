package font

// This file is a narrow cgo binding to HarfBuzz's real text shaping
// (hb_shape) — not OpenType table introspection. ligatures.go uses it to
// answer one question per candidate rune sequence: does this font
// collapse it into a single glyph? HarfBuzz applies whatever GSUB
// machinery the font actually uses to answer that — a direct Ligature
// Substitution (LookupType 4) lookup under 'liga'/'clig'/'dlig', or (far
// more common in real programming-ligature fonts, including the bundled
// FiraCode) a chaining-context lookup under 'calt' that conditionally
// invokes one — the same way any real text shaper (a terminal's own
// line shaping, a text editor) would, and the same way HarfBuzz already
// gets right internally rather than this package re-implementing
// contextual substitution by hand.
//
// Requires libharfbuzz as a system dev package at build time, the same
// way freetype2 already is (see freetype.go) — e.g.
// libharfbuzz-dev/harfbuzz-devel on Linux, `brew install harfbuzz` on
// macOS. Build-time only; the compiled binary links it in.

/*
#cgo pkg-config: harfbuzz
#include <stdlib.h>
#include <hb.h>
*/
import "C"

import "unsafe"

// hbShaper shapes candidate rune sequences through one font's own real
// OpenType layout engine. Kept open across many shapeCollapsesToOne
// calls (see ligatures.go's discoverLigatures, which calls it tens of
// thousands of times over a generated candidate corpus) rather than
// rebuilding its HarfBuzz face/font/buffer per candidate.
type hbShaper struct {
	blob   *C.hb_blob_t
	face   *C.hb_face_t
	font   *C.hb_font_t
	buffer *C.hb_buffer_t
}

func newHBShaper(fontBytes []byte) *hbShaper {
	cdata := C.CBytes(fontBytes)
	blob := C.hb_blob_create((*C.char)(cdata), C.uint(len(fontBytes)), C.HB_MEMORY_MODE_DUPLICATE, nil, nil)
	C.free(cdata) // HarfBuzz already made its own copy (DUPLICATE mode)
	face := C.hb_face_create(blob, 0)
	return &hbShaper{
		blob:   blob,
		face:   face,
		font:   C.hb_font_create(face),
		buffer: C.hb_buffer_create(),
	}
}

func (s *hbShaper) free() {
	C.hb_buffer_destroy(s.buffer)
	C.hb_font_destroy(s.font)
	C.hb_face_destroy(s.face)
	C.hb_blob_destroy(s.blob)
}

// shapeCollapsesToOne shapes runes under a fixed Latin, left-to-right
// context — terminal text is always resolved this way for ligature
// purposes here, regardless of what script guessing from the buffer's
// own (often pure-punctuation) content would otherwise pick — and
// reports the single resulting glyph ID iff the whole sequence collapsed
// into exactly one glyph. HarfBuzz's default shaping already applies
// 'calt'/'liga'/'clig'/'rlig' (and everything else its default GSUB
// feature list includes) with no extra feature list needed.
func (s *hbShaper) shapeCollapsesToOne(runes []rune) (gid uint32, ok bool) {
	C.hb_buffer_reset(s.buffer)
	cps := make([]C.uint32_t, len(runes))
	for i, r := range runes {
		cps[i] = C.uint32_t(r)
	}
	C.hb_buffer_add_utf32(s.buffer, &cps[0], C.int(len(cps)), 0, C.int(len(cps)))
	C.hb_buffer_set_direction(s.buffer, C.HB_DIRECTION_LTR)
	C.hb_buffer_set_script(s.buffer, C.HB_SCRIPT_LATIN)
	C.hb_shape(s.font, s.buffer, nil, 0)

	if C.hb_buffer_get_length(s.buffer) != 1 {
		return 0, false
	}
	info := *C.hb_buffer_get_glyph_infos(s.buffer, nil)
	return uint32(info.codepoint), true
}

// shapeSequence shapes runes under the same fixed Latin/LTR context as
// shapeCollapsesToOne and reports one glyph ID per input rune, in
// order. Unlike shapeCollapsesToOne (which looks for the whole sequence
// merging into a single glyph — a real but less common ligature style),
// this is for fonts that keep one glyph per character and instead
// reshape each one contextually so adjacent glyphs visually connect —
// how FiraCode, Cascadia Code, and JetBrains Mono actually implement
// their arrow/comparison ligatures, verified by shaping "<-" through
// each and finding both glyph IDs change from their isolated-context
// ones while the glyph count stays 2, never collapsing to 1 (see
// ligatures.go's discoverLigatures, which is what actually decides
// whether a same-count reshape is a real ligature rule vs. no-op
// identity shaping). ok is false whenever the shaped result doesn't
// have exactly len(runes) glyphs, or HarfBuzz reordered/merged clusters
// (rare for punctuation-heavy Latin text, but this must not silently
// mis-map a glyph to the wrong input position if it ever happens).
func (s *hbShaper) shapeSequence(runes []rune) (gids []uint32, ok bool) {
	C.hb_buffer_reset(s.buffer)
	cps := make([]C.uint32_t, len(runes))
	for i, r := range runes {
		cps[i] = C.uint32_t(r)
	}
	C.hb_buffer_add_utf32(s.buffer, &cps[0], C.int(len(cps)), 0, C.int(len(cps)))
	C.hb_buffer_set_direction(s.buffer, C.HB_DIRECTION_LTR)
	C.hb_buffer_set_script(s.buffer, C.HB_SCRIPT_LATIN)
	C.hb_shape(s.font, s.buffer, nil, 0)

	n := int(C.hb_buffer_get_length(s.buffer))
	if n != len(runes) {
		return nil, false
	}
	infos := unsafe.Slice(C.hb_buffer_get_glyph_infos(s.buffer, nil), n)
	gids = make([]uint32, n)
	for i, info := range infos {
		if int(info.cluster) != i {
			return nil, false
		}
		gids[i] = uint32(info.codepoint)
	}
	return gids, true
}
