package render

import (
	"fmt"

	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.4/glfw"
)

// Window owns the GLFW window and GL context lifecycle.
type Window struct {
	*glfw.Window
}

func NewWindow(title string, width, height int) (*Window, error) {
	if err := glfw.Init(); err != nil {
		return nil, fmt.Errorf("glfw init: %w", err)
	}
	glfw.WindowHint(glfw.ContextVersionMajor, 3)
	glfw.WindowHint(glfw.ContextVersionMinor, 3)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	// Needed for GL_FRAMEBUFFER_SRGB (enabled below) to have anywhere
	// gamma-correct to write when the CRT pass draws to the default
	// framebuffer — without an sRGB-capable default framebuffer, that
	// enable would only affect the offscreen sRGB FBO the cell pass uses.
	glfw.WindowHint(glfw.SRGBCapable, glfw.True)

	win, err := glfw.CreateWindow(width, height, title, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create window: %w", err)
	}
	win.MakeContextCurrent()

	if err := gl.Init(); err != nil {
		return nil, fmt.Errorf("gl init: %w", err)
	}
	// Mesa dithers the default framebuffer by default to hide 8-bit
	// banding in smooth gradients. Invisible on sharp content, but the
	// soften/bloom passes fill much of the frame with smooth low-contrast
	// gradients, and there the dither pattern reads as a visible fixed
	// grid/noise texture over the whole image — exactly the "grainy,
	// not smooth" look this renderer is trying to avoid.
	gl.Disable(gl.DITHER)
	// No-ops on any draw target that isn't sRGB-format; where the target
	// is (the cell pass's glyph FBO, and the default framebuffer thanks
	// to SRGBCapable above), this makes blending happen in linear light
	// instead of directly on gamma-encoded values — the fix for
	// anti-aliased glyph edges reading thinner/weaker than they should.
	gl.Enable(gl.FRAMEBUFFER_SRGB)
	glfw.SwapInterval(1)
	return &Window{Window: win}, nil
}

func (w *Window) Destroy() {
	w.Window.Destroy()
	glfw.Terminate()
}

func (w *Window) FramebufferPixelSize() (int, int) {
	return w.GetFramebufferSize()
}
