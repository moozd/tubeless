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

// MaxTextureSize reports the current GL context's GL_MAX_TEXTURE_SIZE —
// the largest square texture dimension this GPU/driver will actually
// accept. Callers building a texture whose size depends on user-tunable
// settings (see font.Build's maxTextureSize — the glyph atlas grows with
// atlas.scale and the font's own glyph count) need this to reject an
// oversized request with a clear error up front, rather than letting
// glTexImage2D fail silently and leave every glyph sampling as blank.
// Requires an active GL context (i.e. called after NewWindow, or from
// inside ProbeMaxTextureSize).
func MaxTextureSize() int {
	var v int32
	gl.GetIntegerv(gl.MAX_TEXTURE_SIZE, &v)
	return int(v)
}

// ProbeMaxTextureSize is MaxTextureSize for before any real window
// exists: cmd/tubeless needs this limit to build the glyph atlas at
// startup, but the real window can't be sized until the atlas is built
// (its dimensions come from the atlas's own cell size). This opens a
// throwaway, invisible context just to ask the driver, tears it down,
// and leaves glfw itself initialized (only the real NewWindow's eventual
// Destroy should call glfw.Terminate) so the real window can be created
// right after.
func ProbeMaxTextureSize() (int, error) {
	if err := glfw.Init(); err != nil {
		return 0, fmt.Errorf("glfw init: %w", err)
	}
	glfw.WindowHint(glfw.ContextVersionMajor, 3)
	glfw.WindowHint(glfw.ContextVersionMinor, 3)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	glfw.WindowHint(glfw.Visible, glfw.False)
	defer glfw.DefaultWindowHints()

	probe, err := glfw.CreateWindow(1, 1, "", nil, nil)
	if err != nil {
		return 0, fmt.Errorf("create probe context: %w", err)
	}
	defer probe.Destroy()
	probe.MakeContextCurrent()

	if err := gl.Init(); err != nil {
		return 0, fmt.Errorf("gl init: %w", err)
	}
	return MaxTextureSize(), nil
}

// CurrentMonitorContentScale reports the content scale of whichever
// monitor currently contains the window, found by checking the window's
// centre point against every connected monitor's bounds — unlike the
// window's own GetContentScale (embedded from glfw.Window), which on
// macOS can keep reporting a stale scale after the window is dragged
// from one monitor to another with a different backing scale until some
// unrelated event (a real resize, entering fullscreen) happens to
// refresh GLFW's cached value. Querying the monitor directly sidesteps
// that staleness entirely — this is what runLoop's per-frame
// content-scale check and newRendererFor should use instead of the
// window's own GetContentScale. Falls back to the window's own
// GetContentScale if no monitor's bounds contain it (briefly possible
// mid-drag, or if GetVideoMode fails).
func (w *Window) CurrentMonitorContentScale() (float32, float32) {
	wx, wy := w.GetPos()
	ww, wh := w.GetSize()
	cx, cy := wx+ww/2, wy+wh/2
	for _, m := range glfw.GetMonitors() {
		mx, my := m.GetPos()
		mode := m.GetVideoMode()
		if mode == nil {
			continue
		}
		if cx >= mx && cx < mx+mode.Width && cy >= my && cy < my+mode.Height {
			return m.GetContentScale()
		}
	}
	return w.GetContentScale()
}

// PrimaryMonitorContentScale reports the primary monitor's content
// scale (1 on a standard display, 2 on Retina, etc.) — unlike a
// window's own GetContentScale, this needs no window or GL context at
// all, just glfw initialized, so cmd/tubeless can size the very first
// glyph atlas for the display it's actually about to open on instead of
// blindly assuming 1x and only correcting after the window exists (see
// runLoop's per-frame content-scale check for how a later, genuine
// change — moving to a different monitor — is handled).
func PrimaryMonitorContentScale() (float32, float32, error) {
	if err := glfw.Init(); err != nil {
		return 1, 1, fmt.Errorf("glfw init: %w", err)
	}
	m := glfw.GetPrimaryMonitor()
	if m == nil {
		return 1, 1, nil
	}
	x, y := m.GetContentScale()
	if x <= 0 || y <= 0 {
		return 1, 1, nil
	}
	return x, y, nil
}
