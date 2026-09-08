package render

import (
	"fmt"

	"github.com/go-gl/gl/v3.3-core/gl"
)

// BloomPass builds two layers of glow from a sharp source texture:
//
//   - "soften" blurs the *entire* scene at a modest radius. Crisp, correctly
//     hinted text is exactly what a real CRT never shows you — every edge in
//     the reference photo is soft, not just the bright highlights — so this
//     runs unconditionally, mixed back in by the caller rather than left to
//     a threshold to decide what deserves softening.
//   - "bloom" is a bright-pass (see threshold.frag) blurred more widely and
//     over more iterations, adding extra glow concentrated on genuinely
//     bright elements (a highlighted button, a waveform peak) on top of the
//     base softening, the way saturated phosphor bleeds further than a dim
//     stroke.
type BloomPass struct {
	blurProg   uint32
	threshProg uint32
	vao        uint32

	softenA, softenB *FBO
	softenSpread     float32

	farA, farB *FBO
	farPasses  int
	farSpread  float32
	threshold  float32
}

func NewBloomPass() (*BloomPass, error) {
	blurProg, err := linkProgram(crtVertSrc, blurFragSrc)
	if err != nil {
		return nil, fmt.Errorf("blur program: %w", err)
	}
	threshProg, err := linkProgram(crtVertSrc, thresholdFragSrc)
	if err != nil {
		return nil, fmt.Errorf("threshold program: %w", err)
	}
	return &BloomPass{
		blurProg:     blurProg,
		threshProg:   threshProg,
		vao:          newFullscreenQuadVAO(),
		softenA:      newFBO(2, 2),
		softenB:      newFBO(2, 2),
		softenSpread: 0.8,
		farA:         newFBO(2, 2),
		farB:         newFBO(2, 2),
		farPasses:    2,
		farSpread:    2.0,
		threshold:    0.5,
	}, nil
}

// Compute returns (soften, bloom) textures, both full resolution.
//
// This used to run at half resolution to save GPU work, but the blur
// shader only takes a handful of point/bilinear samples per output texel
// — it doesn't area-average the 2x2 source texels each half-res texel
// represents. For thin 1-2px glyph strokes that's a minification-aliasing
// trap: a dark stroke on a bright reverse-video background (a status bar,
// a selected row) can fall entirely between samples and vanish, which
// read as "reverse video text is unreadable" rather than as a glow bug.
// Running at full resolution removes the aliasing outright; the render
// loop only pays this cost on an actual repaint (see cmd/tubeless's
// dirty-flag loop), not every frame, so the extra work is cheap in
// practice on any GPU that can drive the window at all.
func (b *BloomPass) Compute(sourceTex uint32, srcW, srcH int) (soften, bloom uint32) {
	w, h := max(1, srcW), max(1, srcH)

	b.softenA.Resize(w, h)
	b.softenB.Resize(w, h)
	b.blur(sourceTex, srcW, srcH, b.softenA, b.softenSpread, 0)
	b.blur(b.softenA.tex, w, h, b.softenB, 0, b.softenSpread)

	b.farA.Resize(w, h)
	b.farB.Resize(w, h)
	b.brightPass(sourceTex, b.farA)
	for range b.farPasses {
		b.blur(b.farA.tex, w, h, b.farB, b.farSpread, 0)
		b.blur(b.farB.tex, w, h, b.farA, 0, b.farSpread)
	}

	return b.softenB.tex, b.farA.tex
}

func (b *BloomPass) brightPass(srcTex uint32, dst *FBO) {
	dst.Bind()
	gl.UseProgram(b.threshProg)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, srcTex)
	gl.Uniform1i(gl.GetUniformLocation(b.threshProg, gl.Str("uTexture\x00")), 0)
	gl.Uniform1f(gl.GetUniformLocation(b.threshProg, gl.Str("uThreshold\x00")), b.threshold)
	gl.BindVertexArray(b.vao)
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
	dst.Unbind()
}

// blur runs one separable Gaussian tap over srcTex (sized srcW x srcH) into
// dst, stepping dx/dy texels at a time in srcTex's own texel space.
func (b *BloomPass) blur(srcTex uint32, srcW, srcH int, dst *FBO, dx, dy float32) {
	dst.Bind()
	gl.UseProgram(b.blurProg)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, srcTex)
	gl.Uniform1i(gl.GetUniformLocation(b.blurProg, gl.Str("uTexture\x00")), 0)
	gl.Uniform2f(gl.GetUniformLocation(b.blurProg, gl.Str("uTexelStep\x00")), dx/float32(srcW), dy/float32(srcH))
	gl.BindVertexArray(b.vao)
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
	dst.Unbind()
}
