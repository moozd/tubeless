package render

import (
	"fmt"

	"github.com/go-gl/gl/v3.3-core/gl"
)

// OverlayPass draws the loading modal's dim backdrop, panel and progress bar
// as one fullscreen pass of SDF rounded-rects (see overlay.frag). Text is
// drawn separately by CellPass.DrawTextString over the panel.
type OverlayPass struct {
	prog uint32
	vao  uint32
}

func NewOverlayPass() (*OverlayPass, error) {
	prog, err := linkProgram(fullscreenVertSrc, overlayFragSrc)
	if err != nil {
		return nil, fmt.Errorf("overlay program: %w", err)
	}
	return &OverlayPass{prog: prog, vao: newFullscreenQuadVAO()}, nil
}

// Draw composites the modal shapes onto the default framebuffer. panel/bar
// are pixel rects (x, y, w, h); colors are linear RGB + alpha (see the
// overlay.frag doc for the straight-alpha blend it pairs with).
func (o *OverlayPass) Draw(outW, outH int, panel [4]float32, radius float32, panelColor, borderColor [4]float32, borderWidth float32, bar [4]float32, barRadius float32, track, fill [4]float32, frac, dim float32) {
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
	gl.Viewport(0, 0, int32(outW), int32(outH))
	gl.Enable(gl.BLEND)
	gl.BlendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA)
	gl.UseProgram(o.prog)
	u := func(name string) int32 { return gl.GetUniformLocation(o.prog, gl.Str(name+"\x00")) }
	gl.Uniform2f(u("uRes"), float32(outW), float32(outH))
	gl.Uniform4f(u("uPanel"), panel[0], panel[1], panel[2], panel[3])
	gl.Uniform1f(u("uRadius"), radius)
	gl.Uniform4f(u("uPanelColor"), panelColor[0], panelColor[1], panelColor[2], panelColor[3])
	gl.Uniform4f(u("uBorderColor"), borderColor[0], borderColor[1], borderColor[2], borderColor[3])
	gl.Uniform1f(u("uBorderWidth"), borderWidth)
	gl.Uniform4f(u("uBar"), bar[0], bar[1], bar[2], bar[3])
	gl.Uniform1f(u("uBarRadius"), barRadius)
	gl.Uniform4f(u("uBarTrack"), track[0], track[1], track[2], track[3])
	gl.Uniform4f(u("uBarFill"), fill[0], fill[1], fill[2], fill[3])
	gl.Uniform1f(u("uFrac"), frac)
	gl.Uniform1f(u("uDim"), dim)
	gl.BindVertexArray(o.vao)
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
	gl.Disable(gl.BLEND)
}
