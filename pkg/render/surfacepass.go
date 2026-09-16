package render

import (
	"fmt"

	"github.com/go-gl/gl/v3.3-core/gl"
)

// SurfacePass applies fragment-space corner radius to the literal non-text
// surface texture. It sees pixels only, not terminal cells.
type SurfacePass struct {
	prog uint32
	vao  uint32
}

func NewSurfacePass() (*SurfacePass, error) {
	prog, err := linkProgram(fullscreenVertSrc, surfaceFragSrc)
	if err != nil {
		return nil, fmt.Errorf("surface program: %w", err)
	}
	return &SurfacePass{prog: prog, vao: newFullscreenQuadVAO()}, nil
}

func (s *SurfacePass) Draw(src, dst *FBO, radius, gradient float32) {
	dst.Resize(src.W, src.H)
	dst.Bind()
	gl.UseProgram(s.prog)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, src.tex)
	u := func(name string) int32 { return gl.GetUniformLocation(s.prog, gl.Str(name+"\x00")) }
	gl.Uniform1i(u("uScene"), 0)
	gl.Uniform2f(u("uTexel"), 1.0/float32(src.W), 1.0/float32(src.H))
	gl.Uniform1f(u("uRadius"), radius)
	gl.Uniform1f(u("uGradient"), gradient)
	gl.BindVertexArray(s.vao)
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
	dst.Unbind()
}
