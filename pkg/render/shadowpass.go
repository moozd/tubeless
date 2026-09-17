package render

import (
	"fmt"

	"github.com/go-gl/gl/v3.3-core/gl"
)

// ShadowPass paints a soft, layered "dreamy" drop shadow onto empty
// background around an already rounded surface (see shadow.frag). It runs
// after SurfacePass so the shadow's shape reads the true rounded
// silhouette, not the sharp rectangle underneath it.
type ShadowPass struct {
	prog uint32
	vao  uint32
}

func NewShadowPass() (*ShadowPass, error) {
	prog, err := linkProgram(fullscreenVertSrc, shadowFragSrc)
	if err != nil {
		return nil, fmt.Errorf("shadow program: %w", err)
	}
	return &ShadowPass{prog: prog, vao: newFullscreenQuadVAO()}, nil
}

// radius is surface.frag's own corner radius (see SurfacePass.Draw) —
// threaded through so shadow.frag can scale its search reach to it
// instead of using one flat reach for every surface regardless of size
// (see shadow.frag's shadowReach doc for why that read as radius and
// shadow clashing on small/thin surfaces).
func (s *ShadowPass) Draw(srcTex uint32, dst *FBO, w, h int, shadow, radius float32) {
	dst.Resize(w, h)
	dst.Bind()
	gl.UseProgram(s.prog)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, srcTex)
	u := func(name string) int32 { return gl.GetUniformLocation(s.prog, gl.Str(name+"\x00")) }
	gl.Uniform1i(u("uScene"), 0)
	gl.Uniform2f(u("uTexel"), 1.0/float32(w), 1.0/float32(h))
	gl.Uniform1f(u("uShadow"), shadow)
	gl.Uniform1f(u("uRadius"), radius)
	gl.BindVertexArray(s.vao)
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
	dst.Unbind()
}
