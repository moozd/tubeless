package render

import "github.com/go-gl/gl/v3.3-core/gl"

// OpenTexture is the live, mutable GL texture a `tubeless open` embedded
// app's frame is drawn into: unlike ImagePass's own per-image cache (one
// GL upload, then read-only for the image's lifetime — see getTexture),
// this texture is repeatedly patched in place via UpdateRect as damaged-
// region FrameRect messages arrive (see cmd/tubeless's openserver.go),
// the same way a video frame or a remote desktop viewer updates one
// texture instead of allocating a new one per frame.
type OpenTexture struct {
	tex  uint32
	w, h int
}

// NewOpenTexture allocates the texture object itself (no backing storage
// yet — call Resize once the embedded app's own dimensions are known).
func NewOpenTexture() *OpenTexture {
	var tex uint32
	gl.GenTextures(1, &tex)
	gl.BindTexture(gl.TEXTURE_2D, tex)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	gl.BindTexture(gl.TEXTURE_2D, 0)
	return &OpenTexture{tex: tex}
}

// Resize (re)allocates backing storage at w x h, discarding any existing
// content — called once when a session starts and again on a resize
// (DesktopSize/ExtendedDesktopSize — see pkg/rfbclient.Update.Resized).
// A no-op if the size hasn't actually changed.
func (o *OpenTexture) Resize(w, h int) {
	if w == o.w && h == o.h {
		return
	}
	w, h = max(1, w), max(1, h)
	o.w, o.h = w, h
	gl.BindTexture(gl.TEXTURE_2D, o.tex)
	gl.PixelStorei(gl.UNPACK_ALIGNMENT, 1)
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, int32(w), int32(h), 0, gl.RGBA, gl.UNSIGNED_BYTE, nil)
	gl.BindTexture(gl.TEXTURE_2D, 0)
}

// UpdateRect uploads pix (tightly-packed RGBA8, row-major, exactly
// w*h*4 bytes) into the sub-rectangle at (x, y) — a damaged-region patch,
// not a full re-upload. Must run on the GL thread, same as every other
// render package call.
func (o *OpenTexture) UpdateRect(x, y, w, h int, pix []byte) {
	gl.BindTexture(gl.TEXTURE_2D, o.tex)
	gl.PixelStorei(gl.UNPACK_ALIGNMENT, 1)
	gl.TexSubImage2D(gl.TEXTURE_2D, 0, int32(x), int32(y), int32(w), int32(h), gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(pix))
	gl.BindTexture(gl.TEXTURE_2D, 0)
}

// Width/Height are the texture's current backing-storage dimensions (see
// Resize) — Renderer uses these to decide how to fit the texture into the
// letterboxed content box.
func (o *OpenTexture) Width() int  { return o.w }
func (o *OpenTexture) Height() int { return o.h }
