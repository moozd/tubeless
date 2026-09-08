package render

import (
	"fmt"

	"github.com/go-gl/gl/v3.3-core/gl"

	"github.com/moozd/tubeless/pkg/theme"
)

// CRTPass is the fullscreen post-process shader implementing the phosphor
// glow look (soften + bloom, vignette, optional curvature) — no scanline
// or grain simulation, just smooth phosphor bleed.
type CRTPass struct {
	prog uint32
	vao  uint32
}

func NewCRTPass() (*CRTPass, error) {
	prog, err := linkProgram(crtVertSrc, crtFragSrc)
	if err != nil {
		return nil, fmt.Errorf("crt program: %w", err)
	}
	return &CRTPass{prog: prog, vao: newFullscreenQuadVAO()}, nil
}

func newFullscreenQuadVAO() uint32 {
	verts := []float32{-1, -1, 1, -1, -1, 1, 1, -1, 1, 1, -1, 1}
	var vbo, vao uint32
	gl.GenBuffers(1, &vbo)
	gl.BindBuffer(gl.ARRAY_BUFFER, vbo)
	gl.BufferData(gl.ARRAY_BUFFER, len(verts)*4, gl.Ptr(verts), gl.STATIC_DRAW)
	gl.GenVertexArrays(1, &vao)
	gl.BindVertexArray(vao)
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointerWithOffset(0, 2, gl.FLOAT, false, 2*4, 0)
	gl.BindVertexArray(0)
	return vao
}

// Draw composites sceneTex (the sharp cell-grid FBO), softenTex (the whole
// scene lightly blurred) and bloomTex (a bright-pass-thresholded, more
// widely blurred glow, see BloomPass) to the currently bound target (the
// default framebuffer, sized outW x outH) using th's uniforms.
func (c *CRTPass) Draw(sceneTex, softenTex, bloomTex uint32, th theme.Theme, outW, outH int) {
	gl.UseProgram(c.prog)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, sceneTex)
	gl.ActiveTexture(gl.TEXTURE1)
	gl.BindTexture(gl.TEXTURE_2D, softenTex)
	gl.ActiveTexture(gl.TEXTURE2)
	gl.BindTexture(gl.TEXTURE_2D, bloomTex)
	c.setUniforms(th)
	gl.BindVertexArray(c.vao)
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
}

func (c *CRTPass) setUniforms(th theme.Theme) {
	u := func(name string) int32 { return gl.GetUniformLocation(c.prog, gl.Str(name+"\x00")) }
	gl.Uniform1i(u("uScene"), 0)
	gl.Uniform1i(u("uSoftenTex"), 1)
	gl.Uniform1i(u("uBloomTex"), 2)
	gl.Uniform1f(u("uCurvature"), th.CurvatureAmount)
	gl.Uniform1f(u("uVignette"), th.VignetteStrength)
	gl.Uniform1f(u("uSoften"), th.SoftenAmount)
	gl.Uniform1f(u("uBloom"), th.BloomStrength)
}
