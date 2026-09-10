package render

import (
	"fmt"

	"github.com/go-gl/gl/v3.3-core/gl"

	"github.com/moozd/tubeless/pkg/config"
	"github.com/moozd/tubeless/pkg/font"
	"github.com/moozd/tubeless/pkg/screen"
)

const glyphInstanceFloats = 12    // aCellPos(2) + aUVOffset(2) + aUVSize(2) + aColor(3) + aBgColor(3)
const rectInstanceFloats = 13     // aCellPos(2) + aRectOffset(2) + aRectSize(2) + aColor(3) + aRadius(4)
const underlineInstanceFloats = 6 // aCellPos(2) + aColor(3) + aStyle(1)

// CellPass renders the terminal grid into offscreen FBOs, split into three
// layers: a "rect" layer (background color fills and single-rect block
// glyphs like █▀▄▌▐, drawn procedurally with true rounded corners — see
// cell_rect.frag), a "line-art" layer (box-drawing and powerline glyphs,
// which get a gaussian bloom instead — see shapeblur.frag), and a "text"
// layer (everything else) that stays sharp on top of both.
type CellPass struct {
	atlas            *font.Atlas
	atlasTex         uint32
	progGlyph        uint32
	progRect         uint32
	progUnderline    uint32
	quadVBO          uint32
	glyphVAO         uint32
	glyphInstVBO     uint32
	rectVAO          uint32
	rectInstVBO      uint32
	underlineVAO     uint32
	underlineInstVBO uint32
	shapeScratch     []float32
	textScratch      []float32
	bgScratch        []float32
	blockScratch     []float32
	underlineScratch []float32
	fgCache          [][3]float32
	bgCache          [][3]float32
	cols, rows       int
}

func NewCellPass(atlas *font.Atlas) (*CellPass, error) {
	progGlyph, err := linkProgram(cellGlyphVertSrc, cellGlyphFragSrc)
	if err != nil {
		return nil, fmt.Errorf("glyph program: %w", err)
	}
	progRect, err := linkProgram(cellRectVertSrc, cellRectFragSrc)
	if err != nil {
		return nil, fmt.Errorf("rect program: %w", err)
	}
	progUnderline, err := linkProgram(cellUnderlineVertSrc, cellUnderlineFragSrc)
	if err != nil {
		return nil, fmt.Errorf("underline program: %w", err)
	}
	cp := &CellPass{
		atlas:         atlas,
		atlasTex:      uploadAtlas(atlas),
		progGlyph:     progGlyph,
		progRect:      progRect,
		progUnderline: progUnderline,
	}
	cp.quadVBO = newQuadVBO()
	cp.glyphVAO, cp.glyphInstVBO = newInstancedVAO(cp.quadVBO, glyphInstanceFloats, 3)
	cp.rectVAO, cp.rectInstVBO = newInstancedVAO(cp.quadVBO, rectInstanceFloats, 4)
	cp.underlineVAO, cp.underlineInstVBO = newUnderlineVAO(cp.quadVBO)
	return cp, nil
}

// ensureColorCache sizes fgCache/bgCache for a cols*rows grid, reusing the
// backing array across frames (same reuse pattern as the scratch buffers
// above) rather than reallocating every call.
func (cp *CellPass) ensureColorCache(cols, rows int) {
	n := cols * rows
	if cap(cp.fgCache) < n {
		cp.fgCache = make([][3]float32, n)
		cp.bgCache = make([][3]float32, n)
		return
	}
	cp.fgCache = cp.fgCache[:n]
	cp.bgCache = cp.bgCache[:n]
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
// a second 2-vector + a third 2-vector + aColor(3), plus a trailing
// attribute at location 5 sized extra floats (aBgColor, vec3, for glyph
// instances; aRadius, vec4, for rect instances).
func newInstancedVAO(quadVBO uint32, floats, extra int) (vao, instVBO uint32) {
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
	if extra > 0 {
		attachInstanceAttrib(5, int32(extra), stride, 9*4)
	}

	gl.BindVertexArray(0)
	return vao, instVBO
}

// newUnderlineVAO builds the VAO for underline decoration instances:
// aCellPos(2) + aColor(3) + aStyle(1) — see cell_underline.vert.
func newUnderlineVAO(quadVBO uint32) (vao, instVBO uint32) {
	gl.GenVertexArrays(1, &vao)
	gl.BindVertexArray(vao)

	gl.BindBuffer(gl.ARRAY_BUFFER, quadVBO)
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointerWithOffset(0, 2, gl.FLOAT, false, 2*4, 0)

	gl.GenBuffers(1, &instVBO)
	gl.BindBuffer(gl.ARRAY_BUFFER, instVBO)
	stride := int32(underlineInstanceFloats * 4)
	attachInstanceAttrib(1, 2, stride, 0)   // aCellPos
	attachInstanceAttrib(2, 3, stride, 2*4) // aColor
	attachInstanceAttrib(3, 1, stride, 5*4) // aStyle

	gl.BindVertexArray(0)
	return vao, instVBO
}

func attachInstanceAttrib(loc uint32, size int32, stride int32, offset int) {
	gl.EnableVertexAttribArray(loc)
	gl.VertexAttribPointerWithOffset(loc, size, gl.FLOAT, false, stride, uintptr(offset))
	gl.VertexAttribDivisor(loc, 1)
}

// RowShift nudges every cell in rows [Top,Bottom] (inclusive) by OffsetPx
// physical pixels vertically, on top of their normal y*ch grid position.
// It's how the "content just scrolled" glide (see
// Renderer.ApplyScrollEvents) reads as a continuous slide instead of
// an instant cut: the affected band renders a few pixels off its resting
// position and eases back to zero over a few frames. The zero value is a
// no-op (Bottom < Top matches no row).
type RowShift struct {
	Top, Bottom int
	OffsetPx    float32
}

// BuildInstances reads scr (an immutable published snapshot — see
// cmd/tubeless) into GPU-upload-ready instance buffers. Split from Draw so
// the GPU work (issued by Draw) never needs to touch scr at all. Content is
// routed by kind: background fills and single-rect block glyphs go to the
// rect layer (font.BlockRect), other box-drawing/powerline glyphs go to the
// line-art layer (font.IsShapeRune), everything else is text. The cursor is
// not baked here — it is an animated overlay drawn every frame (see
// CursorPass). scrollOffset (0 = live tail) selects which window of
// scr.VisibleWindow is drawn, for scrollback viewing. sel highlights the
// current mouse selection, if any, by swapping fg/bg for cells inside it
// (see Selection.Contains) — the same reverse-video convention the
// terminal's own SGR 7 already uses for highlights. shift applies the
// in-progress scroll glide, if any (see RowShift).
func (cp *CellPass) BuildInstances(scr *screen.Screen, cfg config.Config, cw, ch float32, scrollOffset int, sel Selection, shift RowShift) {
	cp.bgScratch = cp.bgScratch[:0]
	cp.blockScratch = cp.blockScratch[:0]
	cp.shapeScratch = cp.shapeScratch[:0]
	cp.textScratch = cp.textScratch[:0]
	cp.underlineScratch = cp.underlineScratch[:0]
	cp.cols, cp.rows = scr.Cols, scr.Rows
	atlasW, atlasH := float32(cp.atlas.Image.Bounds().Dx()), float32(cp.atlas.Image.Bounds().Dy())
	empty := ambientBG(cfg)
	grid := scr.VisibleWindow(scrollOffset)
	cp.ensureColorCache(scr.Cols, scr.Rows)

	for y := 0; y < scr.Rows; y++ {
		for x := 0; x < scr.Cols; x++ {
			cell := grid[y][x]
			fg, bg := cellColors(cell.Attr, cfg)
			// Cache this cell's own (unselected) color before applying the
			// selection swap below, so bgRectEdges/blockRectEdges's
			// neighbor lookups (see neighborColors) see exactly what a
			// fresh cellColors call on this cell would have — selection
			// highlighting was never part of that comparison, and caching
			// must not change that.
			i := y*scr.Cols + x
			cp.fgCache[i], cp.bgCache[i] = fg, bg
			if sel.Contains(x, y) {
				fg, bg = bg, fg
			}
			px, py := float32(x)*cw, float32(y)*ch
			if y >= shift.Top && y <= shift.Bottom {
				py += shift.OffsetPx
			}
			if cell.Attr.Underline != screen.UnderlineNone {
				ulColor := underlineColor(cell.Attr, fg, cfg)
				cp.underlineScratch = appendUnderlineInstance(cp.underlineScratch, px, py, ulColor, float32(cell.Attr.Underline))
			}
			if bg != empty {
				e := bgRectEdges(grid, scr.Cols, scr.Rows, cp.fgCache, cp.bgCache, cfg, x, y, bg)
				rx, ry, rw, rh := expandRect(0, 0, 1, 1, cw, ch, e)
				cp.bgScratch = appendRectInstance(cp.bgScratch, px, py, rx, ry, rw, rh, bg, e.Radii)
			}
			if x0, y0, x1, y1, ok := font.BlockRect(cell.Rune); ok {
				e := blockRectEdges(grid, scr.Cols, scr.Rows, cp.fgCache, cp.bgCache, cfg, x, y, cell.Rune, fg, x0, y0, x1, y1)
				rx, ry, rw, rh := expandRect(x0, y0, x1-x0, y1-y0, cw, ch, e)
				cp.blockScratch = appendRectInstance(cp.blockScratch, px, py, rx, ry, rw, rh, fg, e.Radii)
				continue
			}
			g, ok := cp.atlas.Glyphs[cell.Rune]
			if !ok || cell.Rune == ' ' {
				continue
			}
			u0, v0 := float32(g.X)/atlasW, float32(g.Y)/atlasH
			us, vs := float32(g.W)/atlasW, float32(g.H)/atlasH
			if font.IsShapeRune(cell.Rune) {
				cp.shapeScratch = appendGlyphInstance(cp.shapeScratch, px, py, u0, v0, us, vs, fg, bg)
			} else {
				cp.textScratch = appendGlyphInstance(cp.textScratch, px, py, u0, v0, us, vs, fg, bg)
			}
		}
	}
}

func appendRectInstance(dst []float32, px, py, rx, ry, rw, rh float32, color [3]float32, radii [4]float32) []float32 {
	return append(dst, px, py, rx, ry, rw, rh, color[0], color[1], color[2], radii[0], radii[1], radii[2], radii[3])
}

// rectOverlapPx is how far a rect's own geometry overshoots into a
// same-fill neighbor on a squared (radius-0) edge, in physical pixels.
// Each rect instance is anti-aliased independently by cell_rect.frag's own
// SDF; two instances that are merely flush at a shared edge can each fade
// out just short of it and leave a faint seam even though the corner
// there is correctly squared (radius 0). Overlapping by more than the
// SDF's ~1.5px AA fringe guarantees full coverage there regardless of
// sub-pixel rounding in the cell grid's layout.
const rectOverlapPx = 2.0

// expandRect grows a rect (given as cell-fraction offset/size, i.e. what
// aRectOffset/aRectSize become) by rectOverlapPx on whichever edges e
// marks as continuing into a neighbor.
func expandRect(rx, ry, rw, rh, cw, ch float32, e rectEdges) (float32, float32, float32, float32) {
	epsX, epsY := rectOverlapPx/cw, rectOverlapPx/ch
	if e.ContLeft {
		rx -= epsX
		rw += epsX
	}
	if e.ContRight {
		rw += epsX
	}
	if e.ContUp {
		ry -= epsY
		rh += epsY
	}
	if e.ContDown {
		rh += epsY
	}
	return rx, ry, rw, rh
}

func appendGlyphInstance(dst []float32, px, py, u0, v0, us, vs float32, fg, bg [3]float32) []float32 {
	return append(dst, px, py, u0, v0, us, vs, fg[0], fg[1], fg[2], bg[0], bg[1], bg[2])
}

func appendUnderlineInstance(dst []float32, px, py float32, color [3]float32, style float32) []float32 {
	return append(dst, px, py, color[0], color[1], color[2], style)
}

// DrawAmbientBG paints fbo with a single flat-color full-screen rect —
// TrueColor themes' "empty terminal" backdrop (see colors.go's ambientBG).
// Drawn through the normal cell_rect shader pipeline rather than
// gl.ClearColor: a clear bypasses GL_FRAMEBUFFER_SRGB's linear-to-sRGB
// encode (glClear always writes the given value as-is), so a non-zero
// color set that way would be stored as if it were already sRGB-encoded —
// read back too dark once something later samples it expecting real sRGB
// data. A shader write goes through the normal encode step, matching
// every other color in the pipeline. (Plain black, which monochrome
// themes clear straight to, doesn't have this problem: 0 round-trips
// through either encoding unchanged.)
func (cp *CellPass) DrawAmbientBG(fbo *FBO, outW, outH int, color [3]float32) {
	fbo.Bind()
	gl.Disable(gl.BLEND)
	gl.UseProgram(cp.progRect)
	w, h := float32(outW), float32(outH)
	setCommonUniforms(cp.progRect, w, h, w, h, 0, 0)
	inst := appendRectInstance(nil, 0, 0, 0, 0, 1, 1, color, [4]float32{})
	uploadAndDrawInstances(cp.rectVAO, cp.rectInstVBO, inst, rectInstanceFloats)
	fbo.Unbind()
}

// DrawRects clears fbo to transparent and renders background color fills,
// then solid block glyphs on top — both drawn procedurally with true
// rounded corners (see cell_rect.frag). Never blurred itself, but (like
// DrawLineArt) meant to be blurred/bloomed and alpha-composited over the
// scene by the caller (see Renderer.RenderScene's compositeGlow), which
// leaves the crisp interior untouched (premultiplied alpha stays 1 there)
// and only adds a soft glow around the true outer edge.
func (cp *CellPass) DrawRects(fbo *FBO, cw, ch float32) {
	fbo.Bind()
	gl.ClearColor(0, 0, 0, 0)
	gl.Clear(gl.COLOR_BUFFER_BIT)
	gl.Enable(gl.BLEND)
	// Premultiplied alpha (see cell_rect.frag/cell_glyph.frag) — GL_ONE for
	// the source factor is correct over both transparent and opaque
	// destinations, unlike GL_SRC_ALPHA which squares the alpha channel
	// when the destination starts transparent (DrawLineArt's target).
	gl.BlendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA)

	screenW, screenH := float32(fbo.W), float32(fbo.H)
	// The grid's pixel size (cols*cw x rows*ch) only rarely divides the
	// window size evenly; centering means that leftover lands as equal
	// padding on every edge instead of being dumped entirely on the
	// right/bottom, which reads as a stray margin rather than padding.
	offsetX := (screenW - float32(cp.cols)*cw) / 2
	offsetY := (screenH - float32(cp.rows)*ch) / 2

	if len(cp.bgScratch) > 0 {
		cp.drawRects(cp.bgScratch, cw, ch, screenW, screenH, offsetX, offsetY)
	}
	if len(cp.blockScratch) > 0 {
		cp.drawRects(cp.blockScratch, cw, ch, screenW, screenH, offsetX, offsetY)
	}

	gl.Disable(gl.BLEND)
	fbo.Unbind()
}

// DrawLineArt clears fbo to transparent and renders box-drawing/powerline
// glyphs into it — the only content the blur/bloom pass consumes (see
// Renderer.RenderScene, which composites the result over DrawRects's
// output rather than replacing it).
func (cp *CellPass) DrawLineArt(fbo *FBO, cw, ch float32) {
	fbo.Bind()
	gl.ClearColor(0, 0, 0, 0)
	gl.Clear(gl.COLOR_BUFFER_BIT)
	gl.Enable(gl.BLEND)
	// Premultiplied alpha (see cell_rect.frag/cell_glyph.frag) — GL_ONE for
	// the source factor is correct over both transparent and opaque
	// destinations, unlike GL_SRC_ALPHA which squares the alpha channel
	// when the destination starts transparent (DrawLineArt's target).
	gl.BlendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA)

	screenW, screenH := float32(fbo.W), float32(fbo.H)
	offsetX := (screenW - float32(cp.cols)*cw) / 2
	offsetY := (screenH - float32(cp.rows)*ch) / 2
	cp.drawGlyphs(cp.shapeScratch, cw, ch, screenW, screenH, offsetX, offsetY)

	gl.Disable(gl.BLEND)
	fbo.Unbind()
}

// DrawText renders the sharp text glyphs over an existing base (rects +
// composited line-art) in fbo. No clear — the base stays underneath.
func (cp *CellPass) DrawText(fbo *FBO, cw, ch float32) {
	if len(cp.textScratch) == 0 {
		return
	}
	fbo.Bind()
	gl.Enable(gl.BLEND)
	// Premultiplied alpha (see cell_rect.frag/cell_glyph.frag) — GL_ONE for
	// the source factor is correct over both transparent and opaque
	// destinations, unlike GL_SRC_ALPHA which squares the alpha channel
	// when the destination starts transparent (DrawLineArt's target).
	gl.BlendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA)

	screenW, screenH := float32(fbo.W), float32(fbo.H)
	offsetX := (screenW - float32(cp.cols)*cw) / 2
	offsetY := (screenH - float32(cp.rows)*ch) / 2
	cp.drawGlyphs(cp.textScratch, cw, ch, screenW, screenH, offsetX, offsetY)

	gl.Disable(gl.BLEND)
	fbo.Unbind()
}

// DrawUnderline renders underline decorations (single/double/curly/dotted/
// dashed — see screen.UnderlineStyle and cell_underline.frag) over an
// existing base in fbo. No clear, same as DrawText, and drawn before it in
// RenderScene's call order so a glyph's descender (g, y, j) still reads on
// top of the underline band, matching normal terminal compositing.
func (cp *CellPass) DrawUnderline(fbo *FBO, cw, ch float32) {
	if len(cp.underlineScratch) == 0 {
		return
	}
	fbo.Bind()
	gl.Enable(gl.BLEND)
	// Premultiplied alpha — see DrawText's identical blend setup.
	gl.BlendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA)

	screenW, screenH := float32(fbo.W), float32(fbo.H)
	offsetX := (screenW - float32(cp.cols)*cw) / 2
	offsetY := (screenH - float32(cp.rows)*ch) / 2
	gl.UseProgram(cp.progUnderline)
	setCommonUniforms(cp.progUnderline, cw, ch, screenW, screenH, offsetX, offsetY)
	uploadAndDrawInstances(cp.underlineVAO, cp.underlineInstVBO, cp.underlineScratch, underlineInstanceFloats)

	gl.Disable(gl.BLEND)
	fbo.Unbind()
}

func (cp *CellPass) drawGlyphs(instances []float32, cw, ch, screenW, screenH, offsetX, offsetY float32) {
	if len(instances) == 0 {
		return
	}
	gl.UseProgram(cp.progGlyph)
	setCommonUniforms(cp.progGlyph, cw, ch, screenW, screenH, offsetX, offsetY)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, cp.atlasTex)
	gl.Uniform1i(gl.GetUniformLocation(cp.progGlyph, gl.Str("uAtlas\x00")), 0)
	uploadAndDrawInstances(cp.glyphVAO, cp.glyphInstVBO, instances, glyphInstanceFloats)
}

func (cp *CellPass) drawRects(instances []float32, cw, ch, screenW, screenH, offsetX, offsetY float32) {
	gl.UseProgram(cp.progRect)
	setCommonUniforms(cp.progRect, cw, ch, screenW, screenH, offsetX, offsetY)
	uploadAndDrawInstances(cp.rectVAO, cp.rectInstVBO, instances, rectInstanceFloats)
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
