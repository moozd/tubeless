package render

import (
	"fmt"

	"github.com/go-gl/gl/v3.3-core/gl"
)

// surfaceMaxRadius is surface.frag's MAX_REACH: the surface pass never
// rounds a corner more than this, so applet corners stop there too.
const surfaceMaxRadius = 16

// AppletPass draws applet pages with surface.radius corners. Applet
// cells are not in the surface layer (see BuildInstances), so the
// surface passes cannot round or shadow a page by themselves:
// DrawCasters puts a solid rounded block there for the shadow pass,
// and DrawPages clips the page itself to the same shape.
type AppletPass struct {
	prog uint32
	vao  uint32
	span spanUniforms
}

// spanUniforms are the uniforms set once per span, looked up once.
type spanUniforms struct {
	page, pos, size, uvPos, uvSize int32
}

func NewAppletPass() (*AppletPass, error) {
	prog, err := linkProgram(imageVertSrc, appletFragSrc)
	if err != nil {
		return nil, fmt.Errorf("applet program: %w", err)
	}
	p := &AppletPass{prog: prog, vao: newQuadVAO()}
	p.span = spanUniforms{
		page:   p.uniform("uPage"),
		pos:    p.uniform("uPos"),
		size:   p.uniform("uSize"),
		uvPos:  p.uniform("uUVPos"),
		uvSize: p.uniform("uUVSize"),
	}
	return p, nil
}

// appletLayout places applet spans on screen the same way the cell
// passes place cells: cell (col, row) starts at grid + (col, row) * cell.
type appletLayout struct {
	cellW, cellH float32
	gridX, gridY float32 // see GridOffset
	radius       float32 // surface.radius, in pixels
}

// DrawCasters paints each visible page as a solid rounded block of
// color into the surface layer, so the shadow pass casts its shadow.
func (p *AppletPass) DrawCasters(fbo *FBO, spans []AppletSpan, tex map[int]*AppletTexture, l appletLayout, color [3]float32) {
	fill := [4]float32{color[0], color[1], color[2], 1}
	p.draw(fbo, spans, tex, l, fill)
}

// DrawPages paints each span's part of its page over the cells it
// covers, clipped to the page's rounded corners.
func (p *AppletPass) DrawPages(fbo *FBO, spans []AppletSpan, tex map[int]*AppletTexture, l appletLayout) {
	p.draw(fbo, spans, tex, l, [4]float32{})
}

func (p *AppletPass) draw(fbo *FBO, spans []AppletSpan, tex map[int]*AppletTexture, l appletLayout, fill [4]float32) {
	if len(spans) == 0 {
		return
	}
	fbo.Bind()
	gl.Enable(gl.BLEND)
	gl.BlendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA)
	gl.UseProgram(p.prog)
	gl.BindVertexArray(p.vao)
	gl.ActiveTexture(gl.TEXTURE0)
	p.setFrameUniforms(fbo, l.radius, fill)
	for _, s := range spans {
		p.drawSpan(s, tex, l)
	}
	gl.BindVertexArray(0)
	gl.Disable(gl.BLEND)
	fbo.Unbind()
}

func (p *AppletPass) setFrameUniforms(fbo *FBO, radius float32, fill [4]float32) {
	gl.Uniform2f(p.uniform("uScreenSize"), float32(fbo.W), float32(fbo.H))
	gl.Uniform1i(p.uniform("uImage"), 0)
	gl.Uniform1f(p.uniform("uRadius"), min(radius, surfaceMaxRadius))
	gl.Uniform4f(p.uniform("uFill"), fill[0], fill[1], fill[2], fill[3])
}

func (p *AppletPass) drawSpan(s AppletSpan, tex map[int]*AppletTexture, l appletLayout) {
	t, ok := tex[s.ID]
	if !ok || t.w == 0 {
		return
	}
	page := pageRect(s, l)
	gl.BindTexture(gl.TEXTURE_2D, t.tex)
	u := p.span
	gl.Uniform4f(u.page, page[0], page[1], page[2], page[3])
	gl.Uniform2f(u.pos, l.gridX+float32(s.Col)*l.cellW, l.gridY+float32(s.Row)*l.cellH)
	gl.Uniform2f(u.size, float32(s.Cells)*l.cellW, l.cellH)
	gl.Uniform2f(u.uvPos, s.U0, s.V0)
	gl.Uniform2f(u.uvSize, s.U1-s.U0, s.V1-s.V0)
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
}

func (p *AppletPass) uniform(name string) int32 {
	return gl.GetUniformLocation(p.prog, gl.Str(name+"\x00"))
}

// pageRect is the full page's rect in pixels (x, y, w, h), found from
// one span: the span's texture coordinates tell how far the page
// reaches past it on every side, also off screen.
func pageRect(s AppletSpan, l appletLayout) [4]float32 {
	w := float32(s.Cells) * l.cellW / (s.U1 - s.U0)
	h := l.cellH / (s.V1 - s.V0)
	x := l.gridX + float32(s.Col)*l.cellW - s.U0*w
	y := l.gridY + float32(s.Row)*l.cellH - s.V0*h
	return [4]float32{x, y, w, h}
}
