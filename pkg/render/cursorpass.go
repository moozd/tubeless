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
// moves smoothly between cells, but the drawn size is always exactly one
// cell — it never stretches or resizes. offsetX/Y are the cell grid's
// centering offset in pixels (same convention as CellPass.Draw).
// cellW/cellH are the physical pixel size of one cell, cfg supplies the
// cursor color and edge softness, and bright is the breathing-phase
// intensity (0 = hidden).
func (c *CursorPass) Draw(dst *FBO, col, row, offsetX, offsetY, cellW, cellH float32, outW, outH int, bright float32, cfg config.Config) {
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
	gl.BindVertexArray(c.vao)
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
	dst.Unbind()
}
