package render

import (
	"fmt"
	"math"

	"github.com/go-gl/gl/v3.3-core/gl"

	"github.com/moozd/tubeless/pkg/config"
)

// maxTextShadowDepth must match text_shadow.frag's MAX_DEPTH.
const maxTextShadowDepth = 32

// TextShadowPass casts config.TextShadow from a text-only layer onto the
// scene: stacked, optionally blurred and tinted copies of every glyph
// along the shadow offset (see text_shadow.frag). The sharp text is
// drawn over it afterwards, so the glyphs sit on top of their shadow.
type TextShadowPass struct {
	prog uint32
	vao  uint32
}

func NewTextShadowPass() (*TextShadowPass, error) {
	prog, err := linkProgram(fullscreenVertSrc, textShadowFragSrc)
	if err != nil {
		return nil, fmt.Errorf("text shadow program: %w", err)
	}
	return &TextShadowPass{prog: prog, vao: newFullscreenQuadVAO()}, nil
}

// Draw blends the shadow of textTex (premultiplied, over transparent)
// onto dst, which is w x h pixels.
func (p *TextShadowPass) Draw(textTex uint32, dst *FBO, w, h int, s config.TextShadow) {
	dst.Bind()
	gl.Enable(gl.BLEND)
	gl.BlendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA)
	gl.UseProgram(p.prog)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, textTex)
	u := func(name string) int32 { return gl.GetUniformLocation(p.prog, gl.Str(name+"\x00")) }
	texelX, texelY := 1/float32(w), 1/float32(h)
	gl.Uniform1i(u("uText"), 0)
	gl.Uniform2f(u("uTexel"), texelX, texelY)
	// The scene's texture has v = 1 at the top of the screen, so a
	// shadow reaching down the screen is a negative v offset.
	gl.Uniform2f(u("uOffset"), s.OffsetX*texelX, -s.OffsetY*texelY)
	gl.Uniform1i(u("uDepth"), int32(textShadowDepth(s.Depth)))
	gl.Uniform1f(u("uBlur"), max(s.Blur, 0))
	gl.Uniform1f(u("uTint"), clamp01(s.Tint))
	gl.Uniform1f(u("uStrength"), clamp01(s.Strength))
	gl.BindVertexArray(p.vao)
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
	gl.Disable(gl.BLEND)
	dst.Unbind()
}

// textShadowDepth rounds the configured depth to a whole number of
// copies the shader can draw: at least 1, at most maxTextShadowDepth.
func textShadowDepth(depth float32) int {
	n := int(math.Round(float64(depth)))
	return min(max(n, 1), maxTextShadowDepth)
}
