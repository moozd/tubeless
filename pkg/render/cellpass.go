package render

import (
	"fmt"
	"runtime"

	"github.com/go-gl/gl/v3.3-core/gl"

	"github.com/moozd/tubeless/pkg/config"
	"github.com/moozd/tubeless/pkg/font"
	"github.com/moozd/tubeless/pkg/screen"
)

const glyphInstanceFloats = 14    // aCellPos(2) + aUVOffset(2) + aUVSize(2) + aColor(3) + aBgColor(3) + aStyle(1) + aShear(1)
const rectInstanceFloats = 13     // aCellPos(2) + aRectOffset(2) + aRectSize(2) + aColor(3) + aRadius(4)
const underlineInstanceFloats = 6 // aCellPos(2) + aColor(3) + aStyle(1)

// Glyph atlas style indices — which of CellPass's four atlas textures a
// glyph instance samples (aStyle, see appendGlyphInstance/glyphStyle).
// Must match cell_glyph.frag's sampler selection chain.
const (
	styleRegular = iota
	styleBold
	styleItalic
	styleBoldItalic
	styleCount
)

// CellPass renders the terminal grid into offscreen FBOs, split into draw
// buckets so terminal-native ordering stays intact: background fills and
// single-rect block glyphs first, box-drawing/powerline glyphs next, text
// last. Cosmetic effects run later over the completed image.
type CellPass struct {
	faces            *font.Faces
	atlasTex         [styleCount]uint32
	availItalic      bool // Faces.Italic was a real resolved face, not nil
	availBoldItalic  bool // Faces.BoldItalic was a real resolved face, not nil
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
	cols, rows       int
}

func NewCellPass(faces *font.Faces) (*CellPass, error) {
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
		faces:           faces,
		availItalic:     faces.Italic != nil,
		availBoldItalic: faces.BoldItalic != nil,
		progGlyph:       progGlyph,
		progRect:        progRect,
		progUnderline:   progUnderline,
	}
	cp.atlasTex[styleRegular] = uploadAtlas(faces.Regular)
	cp.atlasTex[styleBold] = uploadAtlas(faces.Bold)
	if faces.Italic != nil {
		cp.atlasTex[styleItalic] = uploadAtlas(faces.Italic)
	} else {
		cp.atlasTex[styleItalic] = cp.atlasTex[styleRegular]
	}
	if faces.BoldItalic != nil {
		cp.atlasTex[styleBoldItalic] = uploadAtlas(faces.BoldItalic)
	} else {
		cp.atlasTex[styleBoldItalic] = cp.atlasTex[styleBold]
	}
	cp.quadVBO = newQuadVBO()
	cp.glyphVAO, cp.glyphInstVBO = newGlyphVAO(cp.quadVBO)
	cp.rectVAO, cp.rectInstVBO = newInstancedVAO(cp.quadVBO, rectInstanceFloats, 4)
	cp.underlineVAO, cp.underlineInstVBO = newUnderlineVAO(cp.quadVBO)
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

// newGlyphVAO builds the VAO for glyph instances: aCellPos(2) +
// aUVOffset(2) + aUVSize(2) + aColor(3) + aBgColor(3) + aStyle(1) +
// aShear(1). Its own layout rather than newInstancedVAO's shared "extra"
// slot, since no other instance kind (rect, underline) carries the
// trailing style/shear pair.
func newGlyphVAO(quadVBO uint32) (vao, instVBO uint32) {
	gl.GenVertexArrays(1, &vao)
	gl.BindVertexArray(vao)

	gl.BindBuffer(gl.ARRAY_BUFFER, quadVBO)
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointerWithOffset(0, 2, gl.FLOAT, false, 2*4, 0)

	gl.GenBuffers(1, &instVBO)
	gl.BindBuffer(gl.ARRAY_BUFFER, instVBO)
	stride := int32(glyphInstanceFloats * 4)
	attachInstanceAttrib(1, 2, stride, 0)    // aCellPos
	attachInstanceAttrib(2, 2, stride, 2*4)  // aUVOffset
	attachInstanceAttrib(3, 2, stride, 4*4)  // aUVSize
	attachInstanceAttrib(4, 3, stride, 6*4)  // aColor
	attachInstanceAttrib(5, 3, stride, 9*4)  // aBgColor
	attachInstanceAttrib(6, 1, stride, 12*4) // aStyle
	attachInstanceAttrib(7, 1, stride, 13*4) // aShear

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

// BuildInstances reads scr (an immutable published snapshot — see
// cmd/tubeless) into GPU-upload-ready instance buffers. Split from Draw so
// the GPU work (issued by Draw) never needs to touch scr at all. Content is
// routed by kind only to preserve draw order: background fills and
// single-rect block glyphs go first (font.BlockRect), other
// box-drawing/powerline glyphs next (font.IsShapeRune), everything else is
// text. The cursor is not baked here — it is an animated overlay drawn
// every frame (see CursorPass). scrollOffset (0 = live tail) selects which window of
// scr.VisibleWindow is drawn, for scrollback viewing. sel highlights the
// current mouse selection, if any, by swapping fg/bg for cells inside it
// (see Selection.Contains) — the same reverse-video convention the
// terminal's own SGR 7 already uses for highlights.
func (cp *CellPass) BuildInstances(scr *screen.Screen, cfg config.Config, cw, ch float32, scrollOffset int, sel Selection) {
	cp.bgScratch = cp.bgScratch[:0]
	cp.blockScratch = cp.blockScratch[:0]
	cp.shapeScratch = cp.shapeScratch[:0]
	cp.textScratch = cp.textScratch[:0]
	cp.underlineScratch = cp.underlineScratch[:0]
	cp.cols, cp.rows = scr.Cols, scr.Rows
	empty := ambientBG(cfg)
	grid := scr.VisibleWindow(scrollOffset)

	for y := 0; y < scr.Rows; y++ {
		for x := 0; x < scr.Cols; x++ {
			cell := grid[y][x]
			fg, bg := cellColors(cell.Attr, cfg)
			if sel.Contains(x, y) {
				fg, bg = bg, fg
			}
			px, py := float32(x)*cw, float32(y)*ch
			if cell.Attr.Underline != screen.UnderlineNone {
				ulColor := underlineColor(cell.Attr, fg, cfg)
				cp.underlineScratch = appendUnderlineInstance(cp.underlineScratch, px, py, ulColor, float32(cell.Attr.Underline))
			}
			if bg != empty {
				e := bgRectEdges(grid, scr.Cols, scr.Rows, cfg, x, y, bg)
				rx, ry, rw, rh := expandRect(0, 0, 1, 1, cw, ch, e)
				cp.bgScratch = appendRectInstance(cp.bgScratch, px, py, rx, ry, rw, rh, bg, [4]float32{})
			}
			if x0, y0, x1, y1, ok := font.BlockRect(cell.Rune); ok {
				e := blockRectEdges(grid, scr.Cols, scr.Rows, cfg, x, y, fg, x0, y0, x1, y1)
				rx, ry, rw, rh := expandRect(x0, y0, x1-x0, y1-y0, cw, ch, e)
				cp.blockScratch = appendRectInstance(cp.blockScratch, px, py, rx, ry, rw, rh, fg, [4]float32{})
				continue
			}
			if cell.Rune == ' ' {
				continue
			}
			if font.IsShapeRune(cell.Rune) {
				// Box-drawing/powerline glyphs are synthesized geometry
				// (see font.spriteGlyph), guaranteed present in every
				// atlas identically — always drawn from Regular,
				// unslanted, regardless of the cell's Bold/Italic.
				g, ok := cp.faces.Regular.Glyphs[cell.Rune]
				if !ok {
					continue
				}
				u0, v0, us, vs := glyphUV(cp.faces.Regular, g)
				cp.shapeScratch = appendGlyphInstance(cp.shapeScratch, px, py, u0, v0, us, vs, fg, bg, styleRegular, 0)
				continue
			}
			style, shear := glyphStyle(cell.Attr.Bold, cell.Attr.Italic, cp.availItalic, cp.availBoldItalic)
			atlas := cp.atlasForStyle(style)
			g, ok := atlas.Glyphs[cell.Rune]
			if !ok {
				// The selected style's face doesn't have this rune (e.g.
				// a system Bold cut missing a Nerd Font icon the Regular
				// cut has) — fall back to Regular rather than skipping
				// the glyph entirely.
				atlas, style, shear = cp.faces.Regular, styleRegular, 0
				g, ok = atlas.Glyphs[cell.Rune]
				if !ok {
					continue
				}
			}
			u0, v0, us, vs := glyphUV(atlas, g)
			cp.textScratch = appendGlyphInstance(cp.textScratch, px, py, u0, v0, us, vs, fg, bg, style, shear)
		}
	}
}

// glyphUV converts a Glyph's pixel rect within atlas into normalized
// texture coordinates.
func glyphUV(atlas *font.Atlas, g font.Glyph) (u0, v0, us, vs float32) {
	aw, ah := float32(atlas.Image.Bounds().Dx()), float32(atlas.Image.Bounds().Dy())
	return float32(g.X) / aw, float32(g.Y) / ah, float32(g.W) / aw, float32(g.H) / ah
}

func appendRectInstance(dst []float32, px, py, rx, ry, rw, rh float32, color [3]float32, radii [4]float32) []float32 {
	return append(dst, px, py, rx, ry, rw, rh, color[0], color[1], color[2], radii[0], radii[1], radii[2], radii[3])
}

// rectOverlapPx is how far a rect's own geometry overshoots into a
// same-fill neighbor on a continuing edge, in physical pixels.
// cell_rect.frag antialiases every rect instance independently with a
// ~0.75px SDF fringe; two instances that are merely flush at a shared edge
// (not overlapping) each fade out just short of it and leave a faint seam
// even though nothing should be visible there. Overlapping by more than
// that fringe guarantees full coverage regardless of sub-pixel rounding in
// the cell grid's layout.
const rectOverlapPx = 2.0

// expandRect grows a rect (given as cell-fraction offset/size, i.e. what
// aRectOffset/aRectSize become) by rectOverlapPx on whichever edges e
// marks as continuing into a neighbor.
func expandRect(rx, ry, rw, rh, cw, ch float32, e edgeCont) (float32, float32, float32, float32) {
	epsX, epsY := rectOverlapPx/cw, rectOverlapPx/ch
	if e.Left {
		rx -= epsX
		rw += epsX
	}
	if e.Right {
		rw += epsX
	}
	if e.Up {
		ry -= epsY
		rh += epsY
	}
	if e.Down {
		rh += epsY
	}
	return rx, ry, rw, rh
}

func appendGlyphInstance(dst []float32, px, py, u0, v0, us, vs float32, fg, bg [3]float32, style, shear float32) []float32 {
	return append(dst, px, py, u0, v0, us, vs, fg[0], fg[1], fg[2], bg[0], bg[1], bg[2], style, shear)
}

// glyphStyle picks which atlas a text glyph samples and whether the
// vertex shader should apply a synthetic slant to it, from the cell's
// Bold/Italic attributes and which real faces are actually available.
// Bold is always real (see cmd/tubeless's loadFontFaces — it never
// leaves cp.faces.Bold nil), so only Bold+Italic combinations ever need
// to fall back to a synthesized slant.
func glyphStyle(bold, italic, availItalic, availBoldItalic bool) (style, shear float32) {
	switch {
	case bold && italic:
		if availBoldItalic {
			return styleBoldItalic, 0
		}
		return styleBold, 1
	case bold:
		return styleBold, 0
	case italic:
		if availItalic {
			return styleItalic, 0
		}
		return styleRegular, 1
	default:
		return styleRegular, 0
	}
}

// atlasForStyle returns the Atlas a given style index samples from —
// styleItalic/styleBoldItalic fall back to Regular/Bold when no real
// face was loaded for them (glyphStyle already routes cells away from
// those indices in that case, but BuildInstances' per-glyph fallback
// below needs the same mapping to look up a rune).
func (cp *CellPass) atlasForStyle(style float32) *font.Atlas {
	switch int32(style) {
	case styleBold:
		return cp.faces.Bold
	case styleItalic:
		if cp.faces.Italic != nil {
			return cp.faces.Italic
		}
		return cp.faces.Regular
	case styleBoldItalic:
		if cp.faces.BoldItalic != nil {
			return cp.faces.BoldItalic
		}
		return cp.faces.Bold
	default:
		return cp.faces.Regular
	}
}

func appendUnderlineInstance(dst []float32, px, py float32, color [3]float32, style float32) []float32 {
	return append(dst, px, py, color[0], color[1], color[2], style)
}

// DrawAmbientBG paints the grid's own rect (cols*cw x rows*ch, centered the
// same way DrawRects centers real cell content) with a single flat-color
// fill — TrueColor themes' "empty terminal" backdrop (see colors.go's
// ambientBG). Deliberately NOT a full-screen fill: anything outside that
// rect is Padding.Size's margin (or, with no padding configured, the
// window size's own leftover sub-cell remainder), and leaving it as
// ClearOpaque's plain black rather than painting over it with the same
// color as the grid is what makes that margin read as a distinct bezel
// around the terminal instead of vanishing into "more terminal
// background" — see cmd/tubeless/main.go's pushResizeSize doc comment for
// how padding shrinks cols/rows in the first place.
// Drawn through the normal cell_rect shader pipeline rather than
// gl.ClearColor: a clear bypasses GL_FRAMEBUFFER_SRGB's linear-to-sRGB
// encode (glClear always writes the given value as-is), so a non-zero
// color set that way would be stored as if it were already sRGB-encoded —
// read back too dark once something later samples it expecting real sRGB
// data. A shader write goes through the normal encode step, matching
// every other color in the pipeline. (Plain black, which monochrome
// themes clear straight to, doesn't have this problem: 0 round-trips
// through either encoding unchanged.)
func (cp *CellPass) DrawAmbientBG(fbo *FBO, outW, outH int, cw, ch float32, color [3]float32) {
	fbo.Bind()
	gl.Disable(gl.BLEND)
	gl.UseProgram(cp.progRect)
	screenW, screenH := float32(outW), float32(outH)
	offsetX := (screenW - float32(cp.cols)*cw) / 2
	offsetY := (screenH - float32(cp.rows)*ch) / 2
	setCommonUniforms(cp.progRect, cw, ch, screenW, screenH, offsetX, offsetY)
	inst := appendRectInstance(nil, 0, 0, 0, 0, float32(cp.cols), float32(cp.rows), color, [4]float32{})
	uploadAndDrawInstances(cp.rectVAO, cp.rectInstVBO, inst, rectInstanceFloats)
	fbo.Unbind()
}

// DrawRects renders background color fills, then solid block glyphs on top,
// directly onto the literal scene. No clear and no neighbor-aware geometry:
// cosmetic effects run later as post-processing over the finished image.
func (cp *CellPass) DrawRects(fbo *FBO, cw, ch float32) {
	fbo.Bind()
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

// DrawLineArt renders box-drawing/powerline glyphs directly onto the literal
// scene. The blur/bloom pass sees the completed scene later, not this glyph
// subset as a semantic layer.
func (cp *CellPass) DrawLineArt(fbo *FBO, cw, ch float32) {
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
	// Ghostty's own linear-corrected blend mode (see cell_glyph.frag) is
	// its default everywhere except macOS — the correction assumes the
	// text-weight expectations of gamma-space rendering, which don't hold
	// there. Gated at draw time rather than compiled out, matching every
	// other cross-platform uniform here.
	linearCorrect := int32(0)
	if runtime.GOOS != "darwin" {
		linearCorrect = 1
	}
	gl.Uniform1i(gl.GetUniformLocation(cp.progGlyph, gl.Str("uLinearCorrect\x00")), linearCorrect)
	uniformNames := [styleCount]string{"uAtlas0\x00", "uAtlas1\x00", "uAtlas2\x00", "uAtlas3\x00"}
	for i, tex := range cp.atlasTex {
		gl.ActiveTexture(gl.TEXTURE0 + uint32(i))
		gl.BindTexture(gl.TEXTURE_2D, tex)
		gl.Uniform1i(gl.GetUniformLocation(cp.progGlyph, gl.Str(uniformNames[i])), int32(i))
	}
	uploadAndDrawInstances(cp.glyphVAO, cp.glyphInstVBO, instances, glyphInstanceFloats)
}

func (cp *CellPass) drawRects(instances []float32, cw, ch, screenW, screenH, offsetX, offsetY float32) {
	gl.UseProgram(cp.progRect)
	setCommonUniforms(cp.progRect, cw, ch, screenW, screenH, offsetX, offsetY)
	uploadAndDrawInstances(cp.rectVAO, cp.rectInstVBO, instances, rectInstanceFloats)
}

// DrawTextString draws a short string of glyphs onto the default framebuffer,
// with the first glyph's cell top-left at (x, y) pixels. It's the overlay's
// title/phase/percentage text: drawn while a font rebuild is in flight, it
// always samples the current, still-live atlas. bold selects the Bold cut for
// the title. color is passed for both fg and bg, which makes
// cell_glyph.frag's linear-corrected blend a no-op — correct over an
// arbitrary panel color rather than a flat terminal cell background.
func (cp *CellPass) DrawTextString(text string, x, y, cw, ch float32, color [3]float32, bold bool, outW, outH float32) {
	atlas := cp.faces.Regular
	style := float32(styleRegular)
	if bold {
		atlas = cp.faces.Bold
		style = styleBold
	}
	rs := []rune(text)
	inst := make([]float32, 0, len(rs)*glyphInstanceFloats)
	for i, r := range rs {
		g, ok := atlas.Glyphs[r]
		if !ok {
			continue
		}
		u0, v0, us, vs := glyphUV(atlas, g)
		inst = appendGlyphInstance(inst, x+float32(i)*cw, y, u0, v0, us, vs, color, color, style, 0)
	}
	if len(inst) == 0 {
		return
	}
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
	gl.Viewport(0, 0, int32(outW), int32(outH))
	gl.Enable(gl.BLEND)
	gl.BlendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA)
	cp.drawGlyphs(inst, cw, ch, outW, outH, 0, 0)
	gl.Disable(gl.BLEND)
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
