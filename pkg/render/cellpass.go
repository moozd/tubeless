package render

import (
	"fmt"

	"github.com/go-gl/gl/v3.3-core/gl"

	"github.com/moozd/tubeless/pkg/font"
	"github.com/moozd/tubeless/pkg/screen"
	"github.com/moozd/tubeless/pkg/theme"
)

const bgInstanceFloats = 9 // aCellPos(2) + aUVOffset(2) + aUVSize(2) + aColor(3)

// glyphInstanceFloats adds aBgColor(3) on top of bgInstanceFloats — the
// glyph fragment shader needs the cell's background color (not just its
// own foreground) to compute a gamma-correct blend alpha; see
// cell_glyph.frag.
const glyphInstanceFloats = bgInstanceFloats + 3

// CellPass renders the terminal grid into an offscreen FBO: one instanced
// draw for reverse-video backgrounds, one for glyph coverage.
type CellPass struct {
	atlas        *font.Atlas
	atlasTex     uint32
	progGlyph    uint32
	progBG       uint32
	quadVBO      uint32
	glyphVAO     uint32
	glyphInstVBO uint32
	bgVAO        uint32
	bgInstVBO    uint32
	glyphScratch []float32
	bgScratch    []float32
	cols, rows   int
}

func NewCellPass(atlas *font.Atlas) (*CellPass, error) {
	progGlyph, err := linkProgram(cellGlyphVertSrc, cellGlyphFragSrc)
	if err != nil {
		return nil, fmt.Errorf("glyph program: %w", err)
	}
	progBG, err := linkProgram(cellVertSrc, cellBgFragSrc)
	if err != nil {
		return nil, fmt.Errorf("background program: %w", err)
	}
	cp := &CellPass{
		atlas:     atlas,
		atlasTex:  uploadAtlas(atlas),
		progGlyph: progGlyph,
		progBG:    progBG,
	}
	cp.quadVBO = newQuadVBO()
	cp.glyphVAO, cp.glyphInstVBO = newInstancedVAO(cp.quadVBO, glyphInstanceFloats, true)
	cp.bgVAO, cp.bgInstVBO = newInstancedVAO(cp.quadVBO, bgInstanceFloats, false)
	return cp, nil
}

func newQuadVBO() uint32 {
	verts := []float32{0, 0, 1, 0, 0, 1, 1, 0, 1, 1, 0, 1}
	var vbo uint32
	gl.GenBuffers(1, &vbo)
	gl.BindBuffer(gl.ARRAY_BUFFER, vbo)
	gl.BufferData(gl.ARRAY_BUFFER, len(verts)*4, gl.Ptr(verts), gl.STATIC_DRAW)
	return vbo
}

// newInstancedVAO builds the VAO for either instance layout: aCellPos(2) +
// aUVOffset(2) + aUVSize(2) + aColor(3), plus aBgColor(3) when withBg is
// true (glyph instances only — see cell_glyph.frag).
func newInstancedVAO(quadVBO uint32, floats int, withBg bool) (vao, instVBO uint32) {
	gl.GenVertexArrays(1, &vao)
	gl.BindVertexArray(vao)

	gl.BindBuffer(gl.ARRAY_BUFFER, quadVBO)
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointerWithOffset(0, 2, gl.FLOAT, false, 2*4, 0)

	gl.GenBuffers(1, &instVBO)
	gl.BindBuffer(gl.ARRAY_BUFFER, instVBO)
	stride := int32(floats * 4)
	attachInstanceAttrib(1, 2, stride, 0)
	attachInstanceAttrib(2, 2, stride, 2*4)
	attachInstanceAttrib(3, 2, stride, 4*4)
	attachInstanceAttrib(4, 3, stride, 6*4)
	if withBg {
		attachInstanceAttrib(5, 3, stride, 9*4)
	}

	gl.BindVertexArray(0)
	return vao, instVBO
}

func attachInstanceAttrib(loc uint32, size int32, stride int32, offset int) {
	gl.EnableVertexAttribArray(loc)
	gl.VertexAttribPointerWithOffset(loc, size, gl.FLOAT, false, stride, uintptr(offset))
	gl.VertexAttribDivisor(loc, 1)
}

// BuildInstances reads scr (an immutable published snapshot — see
// cmd/tubeless) into GPU-upload-ready instance buffers. Split from Draw so
// the GPU work (issued by Draw) never needs to touch scr at all.
func (cp *CellPass) BuildInstances(scr *screen.Screen, th theme.Theme, cw, ch float32, cursorOn bool) {
	cp.glyphScratch = cp.glyphScratch[:0]
	cp.bgScratch = cp.bgScratch[:0]
	cp.cols, cp.rows = scr.Cols, scr.Rows
	atlasW, atlasH := float32(cp.atlas.Image.Bounds().Dx()), float32(cp.atlas.Image.Bounds().Dy())

	for y := 0; y < scr.Rows; y++ {
		for x := 0; x < scr.Cols; x++ {
			cell := scr.Grid[y][x]
			fg, bg := cellColors(cell.Attr, th)
			if cursorOn && x == scr.CursorX && y == scr.CursorY {
				fg, bg = [3]float32{0, 0, 0}, th.PhosphorHigh
			}
			px, py := float32(x)*cw, float32(y)*ch
			if bg != ([3]float32{0, 0, 0}) {
				cp.bgScratch = appendInstance(cp.bgScratch, px, py, 0, 0, 1, 1, bg)
			}
			g, ok := cp.atlas.Glyphs[cell.Rune]
			if !ok || cell.Rune == ' ' {
				continue
			}
			u0, v0 := float32(g.X)/atlasW, float32(g.Y)/atlasH
			us, vs := float32(g.W)/atlasW, float32(g.H)/atlasH
			cp.glyphScratch = appendGlyphInstance(cp.glyphScratch, px, py, u0, v0, us, vs, fg, bg)
		}
	}
}

func appendInstance(dst []float32, px, py, u0, v0, us, vs float32, color [3]float32) []float32 {
	return append(dst, px, py, u0, v0, us, vs, color[0], color[1], color[2])
}

func appendGlyphInstance(dst []float32, px, py, u0, v0, us, vs float32, fg, bg [3]float32) []float32 {
	return append(dst, px, py, u0, v0, us, vs, fg[0], fg[1], fg[2], bg[0], bg[1], bg[2])
}

// Draw issues the GPU calls for whatever BuildInstances last captured. It
// touches no Screen state, so it can run after the caller has released
// scr's lock.
func (cp *CellPass) Draw(fbo *FBO, cw, ch float32) {
	fbo.Bind()
	gl.ClearColor(0, 0, 0, 1)
	gl.Clear(gl.COLOR_BUFFER_BIT)
	gl.Enable(gl.BLEND)
	gl.BlendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA)

	screenW, screenH := float32(fbo.W), float32(fbo.H)
	// The grid's pixel size (cols*cw x rows*ch) only rarely divides the
	// window size evenly; centering means that leftover lands as equal
	// padding on every edge instead of being dumped entirely on the
	// right/bottom, which reads as a stray margin rather than padding.
	offsetX := (screenW - float32(cp.cols)*cw) / 2
	offsetY := (screenH - float32(cp.rows)*ch) / 2
	cp.drawBG(cw, ch, screenW, screenH, offsetX, offsetY)
	cp.drawGlyphs(cw, ch, screenW, screenH, offsetX, offsetY)

	gl.Disable(gl.BLEND)
	fbo.Unbind()
}

func (cp *CellPass) drawBG(cw, ch, screenW, screenH, offsetX, offsetY float32) {
	if len(cp.bgScratch) == 0 {
		return
	}
	gl.UseProgram(cp.progBG)
	setCommonUniforms(cp.progBG, cw, ch, screenW, screenH, offsetX, offsetY)
	uploadAndDrawInstances(cp.bgVAO, cp.bgInstVBO, cp.bgScratch, bgInstanceFloats)
}

func (cp *CellPass) drawGlyphs(cw, ch, screenW, screenH, offsetX, offsetY float32) {
	if len(cp.glyphScratch) == 0 {
		return
	}
	gl.UseProgram(cp.progGlyph)
	setCommonUniforms(cp.progGlyph, cw, ch, screenW, screenH, offsetX, offsetY)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, cp.atlasTex)
	gl.Uniform1i(gl.GetUniformLocation(cp.progGlyph, gl.Str("uAtlas\x00")), 0)
	uploadAndDrawInstances(cp.glyphVAO, cp.glyphInstVBO, cp.glyphScratch, glyphInstanceFloats)
}

func setCommonUniforms(prog uint32, cw, ch, screenW, screenH, offsetX, offsetY float32) {
	gl.Uniform2f(gl.GetUniformLocation(prog, gl.Str("uCellSize\x00")), cw, ch)
	gl.Uniform2f(gl.GetUniformLocation(prog, gl.Str("uScreenSize\x00")), screenW, screenH)
	gl.Uniform2f(gl.GetUniformLocation(prog, gl.Str("uOffset\x00")), offsetX, offsetY)
}

func uploadAndDrawInstances(vao, instVBO uint32, data []float32, floats int) {
	gl.BindVertexArray(vao)
	gl.BindBuffer(gl.ARRAY_BUFFER, instVBO)
	gl.BufferData(gl.ARRAY_BUFFER, len(data)*4, gl.Ptr(data), gl.STREAM_DRAW)
	count := int32(len(data) / floats)
	gl.DrawArraysInstanced(gl.TRIANGLES, 0, 6, count)
	gl.BindVertexArray(0)
}
