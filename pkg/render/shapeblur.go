package render

import (
	"fmt"
	"math"

	"github.com/go-gl/gl/v3.3-core/gl"
)

// BlurPass is the separable gaussian bloom over a literal, non-text effect
// source. It runs two fullscreen passes — horizontal into an internal
// scratch FBO, vertical into the destination — so the kernel cost is ~2xN
// taps per pixel instead of NxN. See assets/shaders/shapeblur.frag.
//
// The output is a pure image-space filter; it does not know about cells,
// runs, neighbors or escape sequences.
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
	b.DrawTex(src.tex, dst, radius, strength, src.W, src.H)
}

func (b *BlurPass) DrawTex(srcTex uint32, dst *FBO, radius, strength float32, w, h int) {
	b.draw(srcTex, dst, radius, strength, false, w, h)
}

// DrawGlow writes only the blurred halo of srcTex, scaled by strength,
// into dst with alpha 0 — the source itself is not included. Composited
// with CopyPass.DrawOver it adds light under whatever is drawn next,
// which is how TextGlow sits beneath the sharp text (see RenderScene).
func (b *BlurPass) DrawGlow(srcTex uint32, dst *FBO, radius, strength float32, w, h int) {
	b.draw(srcTex, dst, radius, strength, true, w, h)
}

func (b *BlurPass) draw(srcTex uint32, dst *FBO, radius, strength float32, glowOnly bool, w, h int) {
	if strength <= 0.001 || radius <= 0.01 {
		return
	}
	samples := blurSamples(radius)
	b.tmp.Resize(w, h)
	b.run(srcTex, srcTex, b.tmp, radius, samples, 1.0, 0.0, strength, false, glowOnly, w, h)
	b.run(b.tmp.tex, srcTex, dst, radius, samples, 0.0, 1.0, strength, true, glowOnly, w, h)
}

// blurSamples is the taps-per-side for a gaussian of sigma radius:
// 3 sigma covers ~99.7% of its weight, capped at the shader's loop max.
func blurSamples(radius float32) float32 {
	samples := math.Ceil(3 * float64(radius))
	return float32(math.Max(1, math.Min(16, samples)))
}

// run executes one separable direction: samples tex into dst. orig is the
// texture the glow soft-adds over. final selects whether this pass applies
// that soft-add (the second/vertical pass, against the un-blurred source)
// or just outputs the plain blurred value (the first/horizontal pass) — a
// separable blur's intermediate pass must stay a pure blur, or the second
// pass ends up blurring an already-glow-boosted image and re-applying the
// glow on top of that, compounding into a much stronger (and wrongly
// shaped) effect than the configured strength.
func (b *BlurPass) run(tex, origTex uint32, dst *FBO, radius, samples, dirX, dirY, strength float32, final, glowOnly bool, w, h int) {
	dst.Resize(w, h)
	dst.Bind()
	gl.UseProgram(b.prog)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, tex)
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
	glowOnlyF := float32(0)
	if glowOnly {
		glowOnlyF = 1
	}
	gl.Uniform1f(u("uGlowOnly"), glowOnlyF)
	gl.BindVertexArray(b.vao)
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
	dst.Unbind()
}
