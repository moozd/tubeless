package render

import "github.com/go-gl/gl/v3.3-core/gl"

// FBO is an offscreen color target: the scene the cell pass renders into,
// and the float accumulator the phosphor field is built in.
type FBO struct {
	fbo, tex uint32
	W, H     int
	format   int32
	pixType  uint32
}

// newSRGBFBO is an offscreen sRGB-encoded target: with
// GL_FRAMEBUFFER_SRGB enabled (see window.go), the GPU blends draws into
// it in linear light and stores the sRGB-encoded result, rather than
// blending the raw 0-1 values as if they were already linear. That's what
// gamma-correct glyph antialiasing needs — the cell pass (glyph edges) and
// the sharp scene it feeds use this.
func newSRGBFBO(w, h int) *FBO {
	return newFBOFormat(w, h, gl.SRGB8_ALPHA8, gl.UNSIGNED_BYTE)
}

// newFloatFBO is a high-precision 16-bit float target. The phosphor
// persistence accumulator (see persistpass.go's PersistPass) decays an
// image over many frames; in an 8-bit target that fade quantizes into
// visible steps, while a float target keeps the decay smooth. Values are
// stored as-is (no sRGB encode) — persistence math happens in linear
// light.
func newFloatFBO(w, h int) *FBO {
	return newFBOFormat(w, h, gl.RGBA16F, gl.HALF_FLOAT)
}

func newFBOFormat(w, h int, format int32, pixType uint32) *FBO {
	f := &FBO{W: w, H: h, format: format, pixType: pixType}
	gl.GenFramebuffers(1, &f.fbo)
	gl.GenTextures(1, &f.tex)
	f.allocate(w, h)
	return f
}

func (f *FBO) allocate(w, h int) {
	// A fast interactive-resize drag can momentarily report a 0-sized
	// framebuffer; clamping here (main.go's pushResizeSize already does
	// the same for cols/rows) keeps TexImage2D from ever allocating a
	// degenerate 0x0 texture.
	w, h = max(1, w), max(1, h)
	f.W, f.H = w, h
	gl.BindTexture(gl.TEXTURE_2D, f.tex)
	gl.TexImage2D(gl.TEXTURE_2D, 0, f.format, int32(w), int32(h), 0, gl.RGBA, f.pixType, nil)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)

	gl.BindFramebuffer(gl.FRAMEBUFFER, f.fbo)
	gl.FramebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, f.tex, 0)
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
}

func (f *FBO) Resize(w, h int) {
	if w == f.W && h == f.H {
		return
	}
	f.allocate(w, h)
}

func (f *FBO) Bind() {
	gl.BindFramebuffer(gl.FRAMEBUFFER, f.fbo)
	gl.Viewport(0, 0, int32(f.W), int32(f.H))
}

func (f *FBO) Unbind() {
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
}

// Clear fills the FBO with transparent black.
func (f *FBO) Clear() {
	gl.BindFramebuffer(gl.FRAMEBUFFER, f.fbo)
	gl.ClearColor(0, 0, 0, 0)
	gl.Clear(gl.COLOR_BUFFER_BIT)
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
}

// ClearOpaque fills the FBO with opaque black — the "empty terminal" base
// the scene's rect/line-art layers then composite glowing content over.
func (f *FBO) ClearOpaque() {
	gl.BindFramebuffer(gl.FRAMEBUFFER, f.fbo)
	gl.ClearColor(0, 0, 0, 1)
	gl.Clear(gl.COLOR_BUFFER_BIT)
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
}
