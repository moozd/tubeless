package render

import (
	"fmt"

	"github.com/go-gl/gl/v3.3-core/gl"

	"github.com/moozd/tubeless/pkg/config"
)

// CursorPass draws the animated cursor block (see assets/shaders/cursor.frag)
// into a dedicated FBO that the persistence pass soft-adds over the scene.
// It runs every visible frame so the cursor can glide between cells and
// breathe, independent of the dirty-gated scene rebuild.
type CursorPass struct {
	prog uint32
	vao  uint32
}

func NewCursorPass() (*CursorPass, error) {
	prog, err := linkProgram(fullscreenVertSrc, cursorFragSrc)
	if err != nil {
		return nil, fmt.Errorf("cursor program: %w", err)
	}
	return &CursorPass{prog: prog, vao: newFullscreenQuadVAO()}, nil
}

// Draw renders the cursor at col/row — in grid coordinates, i.e. before
// the centering offset — into dst, which covers the whole window.
// col/row come from Renderer.UpdateCursor's eased glide, so the position
// moves smoothly between cells; the head shape stays within that one
// cell's footprint, but morph (0 = at-rest shape, 1 = full ball+tail) can
// stream a tapering tail out past it along tailDirX/Y (a screen-space
// unit vector), tailLen pixels long — see cursor.frag. offsetX/Y are the
// cell grid's centering offset in pixels (same convention as
// CellPass.Draw). cellW/cellH are the physical pixel size of one cell,
// cfg supplies the cursor color/shape/radius/glass settings, bright is
// the breathing-phase intensity (0 = hidden), and sceneTex is the sharp
// scene texture — sampled only when cfg.Cursor.Glass.Enabled, for the
// frosted-glass refraction.
func (c *CursorPass) Draw(dst *FBO, col, row, offsetX, offsetY, cellW, cellH float32, outW, outH int, bright, morph, tailDirX, tailDirY, tailLen float32, sceneTex uint32, cfg config.Config) {
	pos := [2]float32{offsetX + col*cellW, offsetY + row*cellH}
	size := [2]float32{cellW, cellH}

	dst.Resize(outW, outH)
	dst.Bind()
	gl.UseProgram(c.prog)
	u := func(name string) int32 { return gl.GetUniformLocation(c.prog, gl.Str(name+"\x00")) }
	gl.Uniform2f(u("uPos"), pos[0], pos[1])
	gl.Uniform2f(u("uSize"), size[0], size[1])
	gl.Uniform2f(u("uScreenSize"), float32(outW), float32(outH))
	gl.Uniform1f(u("uBright"), bright)
	gl.Uniform1f(u("uGlow"), cfg.Cursor.Glow)
	gl.Uniform3fv(u("uAccent"), 1, &cfg.Phosphor.High[0])
	gl.Uniform1f(u("uMorph"), morph)
	gl.Uniform2f(u("uTailDir"), tailDirX, tailDirY)
	gl.Uniform1f(u("uTailLen"), tailLen)
	gl.Uniform1f(u("uShape"), cursorShapeIndex(cfg.Cursor.Shape))
	gl.Uniform1f(u("uRadius"), cfg.Cursor.Radius)

	glass := cfg.Cursor.Glass
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, sceneTex)
	gl.Uniform1i(u("uScene"), 0)
	if glass.Enabled {
		gl.Uniform1f(u("uGlassMode"), 1)
	} else {
		gl.Uniform1f(u("uGlassMode"), 0)
	}
	gl.Uniform1f(u("uGlassTint"), glass.Tint)
	gl.Uniform1f(u("uGlassBlur"), glass.Blur)
	gl.Uniform1f(u("uGlassRefract"), glass.Refract)
	gl.Uniform1f(u("uGlassOpacity"), glass.Opacity)

	gl.BindVertexArray(c.vao)
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
	dst.Unbind()
}

// cursorShapeIndex maps a Cursor.Shape config string to cursor.frag's
// uShape (0 = block, 1 = bar, 2 = underline). An unrecognized or empty
// name falls back to block.
func cursorShapeIndex(shape string) float32 {
	switch shape {
	case "bar":
		return 1
	case "underline":
		return 2
	default:
		return 0
	}
}
