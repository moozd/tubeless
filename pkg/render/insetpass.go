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
// tint/shadow/CRT-effect parameters. elapsed is wrapped elapsed seconds,
// for the effects (noise, flicker) that vary over time.
func (i *InsetPass) Draw(sceneTex, cursorTex uint32, cfg config.Config, outW, outH int, elapsed float64) {
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
	x, y, w, h := LetterboxBox(cfg.CRT.AspectRatio, outW, outH)
	if w != outW || h != outH {
		// Letterboxed: the box (w,h) covers less than the window, so the
		// bars around it need painting black first — a plain glViewport
		// to the smaller box would otherwise leave whatever was in that
		// framebuffer region from a previous frame.
		gl.Viewport(0, 0, int32(outW), int32(outH))
		gl.ClearColor(0, 0, 0, 1)
		gl.Clear(gl.COLOR_BUFFER_BIT)
	}
	gl.Viewport(int32(x), int32(y), int32(w), int32(h))
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
	gl.Uniform1f(u("uAspect"), float32(w)/float32(max(h, 1)))
	gl.Uniform1f(u("uTime"), float32(elapsed))
	crt := cfg.CRT
	gl.Uniform1f(u("uCurvature"), crt.Curvature.Amount)
	gl.Uniform1f(u("uAberration"), crt.Aberration.Amount)
	gl.Uniform1f(u("uScanIntensity"), crt.Scanlines.Intensity)
	gl.Uniform1f(u("uScanPeriod"), crt.Scanlines.Period)
	gl.Uniform1f(u("uMaskIntensity"), crt.ShadowMask.Intensity)
	gl.Uniform1f(u("uMaskCellSize"), crt.ShadowMask.CellSize)
	gl.Uniform1f(u("uNoiseIntensity"), crt.Noise.Intensity)
	gl.Uniform1f(u("uFlickerAmount"), crt.Flicker.Amount)
	gl.Uniform1f(u("uFlickerSpeed"), crt.Flicker.Speed)
	gl.BindVertexArray(i.vao)
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
}

// LetterboxBox returns the centered box within outW x outH that matches
// ar's width:height ratio — see AspectRatio's own doc comment. (0, 0,
// outW, outH), filling the window exactly, when ar is unset (either
// field <= 0) — every non-monitor preset, "modern" included. Exported so
// cmd/tubeless can size the terminal's own column/row grid against the
// same box InsetPass.Draw composites into — computed the same way in
// both places, they always agree exactly, so the scene texture that
// grid produces lands in that viewport at 1:1, never resampled/stretched
// to a different shape (see pushResizeSize's own doc comment).
func LetterboxBox(ar config.AspectRatio, outW, outH int) (x, y, w, h int) {
	if ar.Width <= 0 || ar.Height <= 0 {
		return 0, 0, outW, outH
	}
	target := ar.Width / ar.Height
	winAspect := float32(outW) / float32(max(outH, 1))
	if winAspect > target {
		h = outH
		w = int(float32(outH) * target)
		x = (outW - w) / 2
	} else {
		w = outW
		h = int(float32(outW) / target)
		y = (outH - h) / 2
	}
	return x, y, w, h
}
