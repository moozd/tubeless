package render

import "github.com/go-gl/gl/v3.3-core/gl"

// SlotTexture is the live, mutable GL texture one embedded GUI app's
// frame is drawn into. Unlike ImagePass's per-image cache (one upload,
// read-only afterwards), it is patched in place as damaged rects arrive
// from the app's compositor, the way a video frame or remote desktop
// viewer updates a single texture instead of allocating one per frame.
// Pixels are BGRA, exactly as the compositor's shared buffer holds them.
type SlotTexture struct {
	tex  uint32
	w, h int
}

// NewSlotTexture allocates the texture object itself, with no backing
// storage yet: call Resize once the app's frame size is known.
func NewSlotTexture() *SlotTexture {
	var tex uint32
	gl.GenTextures(1, &tex)
	gl.BindTexture(gl.TEXTURE_2D, tex)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	gl.BindTexture(gl.TEXTURE_2D, 0)
	return &SlotTexture{tex: tex}
}

// Resize (re)allocates backing storage at w x h, discarding the old
// content. A no-op if the size hasn't changed.
func (t *SlotTexture) Resize(w, h int) {
	w, h = max(1, w), max(1, h)
	if w == t.w && h == t.h {
		return
	}
	t.w, t.h = w, h
	gl.BindTexture(gl.TEXTURE_2D, t.tex)
	gl.PixelStorei(gl.UNPACK_ALIGNMENT, 1)
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.SRGB8_ALPHA8, int32(w), int32(h), 0, gl.BGRA, gl.UNSIGNED_BYTE, nil)
	gl.BindTexture(gl.TEXTURE_2D, 0)
}

// UpdateRect uploads the sub-rectangle at (x, y), w x h pixels, from pix
// (BGRA, rowLen pixels per row, so it can point straight into a shared
// frame buffer). Must run on the GL thread.
func (t *SlotTexture) UpdateRect(x, y, w, h int, pix []byte, rowLen int) {
	gl.BindTexture(gl.TEXTURE_2D, t.tex)
	gl.PixelStorei(gl.UNPACK_ALIGNMENT, 1)
	gl.PixelStorei(gl.UNPACK_ROW_LENGTH, int32(rowLen))
	first := (y*rowLen + x) * 4
	gl.TexSubImage2D(gl.TEXTURE_2D, 0, int32(x), int32(y), int32(w), int32(h), gl.BGRA, gl.UNSIGNED_BYTE, gl.Ptr(&pix[first]))
	gl.PixelStorei(gl.UNPACK_ROW_LENGTH, 0)
	gl.BindTexture(gl.TEXTURE_2D, 0)
}

func (t *SlotTexture) Width() int  { return t.w }
func (t *SlotTexture) Height() int { return t.h }

func (t *SlotTexture) delete() {
	gl.DeleteTextures(1, &t.tex)
}

// SlotSpan is a horizontal run of still-visible cells of one embedded
// app, and the part of its texture (in 0..1 coordinates) those cells
// show. Cells covered by a tmux popup are simply absent from the list.
type SlotSpan struct {
	ID             int
	Col, Row       int
	Cells          int
	U0, V0, U1, V1 float32
}
