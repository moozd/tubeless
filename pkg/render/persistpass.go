package render

import (
	"fmt"
	"math"

	"github.com/go-gl/gl/v3.3-core/gl"
)

// PersistPass implements phosphor persistence (see config.PhosphorDecay):
// each frame's scene is soft-added over a decayed copy of the previous
// frame's accumulated image, producing an afterglow trail on content that
// just changed. It needs a ping-pong pair of float FBOs (see
// Renderer.persistFBO) since a texture can't be read and written in the
// same draw call — Step always reads from one and writes the other.
type PersistPass struct {
	prog uint32
	vao  uint32
}

func NewPersistPass() (*PersistPass, error) {
	prog, err := linkProgram(fullscreenVertSrc, decayFragSrc)
	if err != nil {
		return nil, fmt.Errorf("decay program: %w", err)
	}
	return &PersistPass{prog: prog, vao: newFullscreenQuadVAO()}, nil
}

// Step blends scene over the previous accumulator (accum[readIdx]),
// decayed by decaySeconds over dt, into accum[1-readIdx], and returns that
// index plus its texture. Only called when decaySeconds > 0 — the caller
// falls back to the raw scene texture otherwise, so a disabled decay costs
// nothing beyond the two idle float FBOs.
func (p *PersistPass) Step(scene *FBO, accum *[2]*FBO, readIdx int, decaySeconds float32, dt float64, outW, outH int) (writeIdx int, tex uint32) {
	writeIdx = 1 - readIdx
	dst := accum[writeIdx]
	dst.Resize(outW, outH)
	dst.Bind()
	gl.UseProgram(p.prog)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, scene.tex)
	gl.ActiveTexture(gl.TEXTURE1)
	gl.BindTexture(gl.TEXTURE_2D, accum[readIdx].tex)
	u := func(name string) int32 { return gl.GetUniformLocation(p.prog, gl.Str(name+"\x00")) }
	gl.Uniform1i(u("uScene"), 0)
	gl.Uniform1i(u("uPrevAccum"), 1)
	gl.Uniform1f(u("uDecay"), decayFactor(decaySeconds, dt))
	gl.BindVertexArray(p.vao)
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
	dst.Unbind()
	return writeIdx, dst.tex
}

// decayFactor is the per-frame multiplier that takes a full-brightness
// pixel down to ~5% residual after decaySeconds of no further input.
func decayFactor(decaySeconds float32, dt float64) float32 {
	if decaySeconds <= 0 {
		return 0
	}
	return float32(math.Pow(0.05, dt/float64(decaySeconds)))
}
