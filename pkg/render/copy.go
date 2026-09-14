package render

import (
	"fmt"

	"github.com/go-gl/gl/v3.3-core/gl"
)

// CopyPass copies a texture into a framebuffer 1:1 (see copy.frag).
// Sampling an sRGB texture decodes to linear and writing to an sRGB target
// re-encodes, so the round-trip is lossless.
type CopyPass struct {
	prog uint32
	vao  uint32
}

func NewCopyPass() (*CopyPass, error) {
	prog, err := linkProgram(fullscreenVertSrc, copyFragSrc)
	if err != nil {
		return nil, fmt.Errorf("copy program: %w", err)
	}
	return &CopyPass{prog: prog, vao: newFullscreenQuadVAO()}, nil
}

func (c *CopyPass) Draw(srcTex uint32, dst *FBO, w, h int) {
	dst.Resize(w, h)
	dst.Bind()
	gl.UseProgram(c.prog)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, srcTex)
	gl.Uniform1i(gl.GetUniformLocation(c.prog, gl.Str("uScene\x00")), 0)
	gl.BindVertexArray(c.vao)
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
	dst.Unbind()
}

// DrawOver alpha-blends srcTex onto dst's existing content, instead of
// Draw's plain overwrite. srcTex holds premultiplied alpha, so the blend
// uses GL_ONE for the source factor rather than GL_SRC_ALPHA.
func (c *CopyPass) DrawOver(srcTex uint32, dst *FBO, w, h int) {
	dst.Resize(w, h)
	dst.Bind()
	gl.Enable(gl.BLEND)
	gl.BlendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA)
	gl.UseProgram(c.prog)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, srcTex)
	gl.Uniform1i(gl.GetUniformLocation(c.prog, gl.Str("uScene\x00")), 0)
	gl.BindVertexArray(c.vao)
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
	gl.Disable(gl.BLEND)
	dst.Unbind()
}
