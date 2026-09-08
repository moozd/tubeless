package render

import "github.com/go-gl/gl/v3.3-core/gl"

// FBO is an offscreen color target the cell pass renders into and the CRT
// pass samples from.
type FBO struct {
	fbo, tex uint32
	W, H     int
	format   int32
}

func newFBO(w, h int) *FBO {
	return newFBOFormat(w, h, gl.RGBA8)
}

// newSRGBFBO is like newFBO, but the texture is sRGB-encoded: with
// GL_FRAMEBUFFER_SRGB enabled (see window.go), the GPU blends draws into
// it in linear light and stores the sRGB-encoded result, rather than
// blending the raw 0-1 values as if they were already linear. That's what
// gamma-correct glyph antialiasing needs — the cell pass (glyph edges)
// uses this; the bloom/blur intermediates stay plain RGBA8 since they're
// not blending partial-coverage edges.
func newSRGBFBO(w, h int) *FBO {
	return newFBOFormat(w, h, gl.SRGB8_ALPHA8)
}

func newFBOFormat(w, h int, format int32) *FBO {
	f := &FBO{W: w, H: h, format: format}
	gl.GenFramebuffers(1, &f.fbo)
	gl.GenTextures(1, &f.tex)
	f.allocate(w, h)
	return f
}

func (f *FBO) allocate(w, h int) {
	f.W, f.H = w, h
	gl.BindTexture(gl.TEXTURE_2D, f.tex)
	gl.TexImage2D(gl.TEXTURE_2D, 0, f.format, int32(w), int32(h), 0, gl.RGBA, gl.UNSIGNED_BYTE, nil)
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
