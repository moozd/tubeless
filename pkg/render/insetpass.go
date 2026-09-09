package render

import (
	"fmt"

	"github.com/go-gl/gl/v3.3-core/gl"

	"github.com/moozd/tubeless/pkg/config"
)

// InsetPass is the final composite: it adds the animated cursor glow over
// the scene, tints the dead-black parts to the config's accent (a scope
// screen is never pure black) and applies the radial tube-face falloff that
// darkens the screen toward its corners (see assets/shaders/inset.frag).
type InsetPass struct {
	prog uint32
	vao  uint32
}

func NewInsetPass() (*InsetPass, error) {
	prog, err := linkProgram(fullscreenVertSrc, insetFragSrc)
	if err != nil {
		return nil, fmt.Errorf("inset program: %w", err)
	}
	return &InsetPass{prog: prog, vao: newFullscreenQuadVAO()}, nil
}

// Draw composites the sharp scene (sceneTex) with the cursor glow
// (cursorTex) to the default framebuffer, sized outW x outH, using cfg's
// tint and shadow parameters.
func (i *InsetPass) Draw(sceneTex, cursorTex uint32, cfg config.Config, outW, outH int) {
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
	gl.Viewport(0, 0, int32(outW), int32(outH))
	gl.UseProgram(i.prog)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, sceneTex)
	gl.ActiveTexture(gl.TEXTURE1)
	gl.BindTexture(gl.TEXTURE_2D, cursorTex)
	u := func(name string) int32 { return gl.GetUniformLocation(i.prog, gl.Str(name+"\x00")) }
	gl.Uniform1i(u("uScene"), 0)
	gl.Uniform1i(u("uCursor"), 1)
	gl.Uniform3fv(u("uAccent"), 1, &cfg.Phosphor.Low[0])
	gl.Uniform1f(u("uBgTint"), cfg.Face.BgTint)
	gl.Uniform1f(u("uInsetShadow"), cfg.Face.InsetShadow)
	gl.Uniform1f(u("uAspect"), float32(outW)/float32(max(outH, 1)))
	gl.BindVertexArray(i.vao)
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
}
