package render

import (
	"fmt"

	"github.com/go-gl/gl/v3.3-core/gl"
)

// signatureTaps is how many evenly-spaced samples SignaturePass averages
// per signature slice — see signature.frag's uTaps. 16 is a middle
// ground: enough taps to smooth over per-glyph antialiasing noise
// without needing a large, driver-portability-risking loop bound (the
// shader's hard cap is 64).
const signatureTaps = 16

// SignaturePass reduces a rendered frame down to a small per-row or
// per-column pixel "signature" texture (see imagediff.go, which is the
// only caller) — one output texel per source row/column, each an
// average of signatureTaps samples along the OTHER axis. The axis being
// kept (row index for a row signature, column index for a column
// signature) is never blended across its own boundary: shift detection
// needs single-row/column precision, so averaging into a neighbor would
// blur exactly the signal being measured.
type SignaturePass struct {
	prog uint32
	vao  uint32
}

func NewSignaturePass() (*SignaturePass, error) {
	prog, err := linkProgram(fullscreenVertSrc, signatureFragSrc)
	if err != nil {
		return nil, fmt.Errorf("signature program: %w", err)
	}
	return &SignaturePass{prog: prog, vao: newFullscreenQuadVAO()}, nil
}

// DrawRowSignature reduces srcTex to a sigLen x rows texture in dst: one
// output row per source row (fixed Y, no blending across rows),
// averaging sigLen horizontal slices across the row's width.
func (p *SignaturePass) DrawRowSignature(srcTex uint32, dst *FBO, sigLen, rows int) {
	dst.Resize(sigLen, rows)
	p.draw(srcTex, dst, float32(sigLen), 0)
}

// DrawColSignature is DrawRowSignature's transpose: a cols x sigLen
// texture, one output column per source column (fixed X), averaging
// sigLen vertical slices across the column's height.
func (p *SignaturePass) DrawColSignature(srcTex uint32, dst *FBO, cols, sigLen int) {
	dst.Resize(cols, sigLen)
	p.draw(srcTex, dst, float32(sigLen), 1)
}

func (p *SignaturePass) draw(srcTex uint32, dst *FBO, sigLen, axis float32) {
	dst.Bind()
	gl.UseProgram(p.prog)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, srcTex)
	u := func(name string) int32 { return gl.GetUniformLocation(p.prog, gl.Str(name+"\x00")) }
	gl.Uniform1i(u("uScene"), 0)
	gl.Uniform1f(u("uSigLen"), sigLen)
	gl.Uniform1f(u("uTaps"), float32(signatureTaps))
	gl.Uniform1f(u("uAxis"), axis)
	gl.BindVertexArray(p.vao)
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
	dst.Unbind()
}
