package render

import (
	"fmt"
	"math"

	"github.com/go-gl/gl/v3.3-core/gl"
)

// BlurPass is the separable gaussian bloom that softens the shape layer's
// line art (box-drawing + powerline glyphs — solid blocks and backgrounds
// are rounded geometrically instead, see cell_bg.frag). It runs two
// fullscreen passes — horizontal into an internal scratch FBO, vertical
// into the destination — so the kernel cost is ~2xN taps per pixel instead
// of NxN. See assets/shaders/shapeblur.frag.
//
// It only ever runs when the scene changes (Renderer.RenderScene); the
// output is a pure function of the current shape layer.
type BlurPass struct {
	prog uint32
	vao  uint32
	tmp  *FBO
}

func NewBlurPass() (*BlurPass, error) {
	prog, err := linkProgram(fullscreenVertSrc, shapeBlurFragSrc)
	if err != nil {
		return nil, fmt.Errorf("shape blur program: %w", err)
	}
	return &BlurPass{prog: prog, vao: newFullscreenQuadVAO(), tmp: newSRGBFBO(2, 2)}, nil
}

// Draw blurs src into dst. radius is the gaussian sigma in texels and
// strength (0..1) is the intensity of the glow soft-added back over the
// original (0 = passthrough). The intermediate horizontal pass lands in
// b.tmp; the soft-add happens on the vertical pass against src itself.
func (b *BlurPass) Draw(src, dst *FBO, radius, strength float32) {
	if strength <= 0.001 || radius <= 0.01 {
		return
	}
	sigma := float64(radius)
	samples := int(math.Ceil(3 * sigma))
	if samples < 1 {
		samples = 1
	}
	if samples > 16 {
		samples = 16
	}

	b.tmp.Resize(src.W, src.H)
	b.run(src, src.tex, b.tmp, radius, float32(samples), 1.0, 0.0, strength, false)
	b.run(b.tmp, src.tex, dst, radius, float32(samples), 0.0, 1.0, strength, true)
}

// run executes one separable direction: samples tex into dst. orig is the
// texture the glow soft-adds over. final selects whether this pass applies
// that soft-add (the second/vertical pass, against the un-blurred source)
// or just outputs the plain blurred value (the first/horizontal pass) — a
// separable blur's intermediate pass must stay a pure blur, or the second
// pass ends up blurring an already-glow-boosted image and re-applying the
// glow on top of that, compounding into a much stronger (and wrongly
// shaped) effect than the configured strength.
func (b *BlurPass) run(tex *FBO, origTex uint32, dst *FBO, radius, samples, dirX, dirY, strength float32, final bool) {
	w, h := tex.W, tex.H
	dst.Resize(w, h)
	dst.Bind()
	gl.UseProgram(b.prog)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, tex.tex)
	gl.ActiveTexture(gl.TEXTURE1)
	gl.BindTexture(gl.TEXTURE_2D, origTex)
	u := func(name string) int32 { return gl.GetUniformLocation(b.prog, gl.Str(name+"\x00")) }
	gl.Uniform1i(u("uScene"), 0)
	gl.Uniform1i(u("uOrig"), 1)
	gl.Uniform2f(u("uTexel"), 1.0/float32(w), 1.0/float32(h))
	gl.Uniform1f(u("uSigma"), radius)
	gl.Uniform1f(u("uSamples"), samples)
	gl.Uniform2f(u("uDir"), dirX, dirY)
	gl.Uniform1f(u("uStrength"), strength)
	finalF := float32(0)
	if final {
		finalF = 1
	}
	gl.Uniform1f(u("uFinal"), finalF)
	gl.BindVertexArray(b.vao)
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
	dst.Unbind()
}
