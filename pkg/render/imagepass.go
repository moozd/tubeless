package render

import (
	"fmt"
	"image"

	"github.com/go-gl/gl/v3.3-core/gl"

	"github.com/moozd/tubeless/pkg/screen"
)

// ImagePass draws sixel-decoded images at their placed cell position into
// the same offscreen FBO the cell pass renders text into, so they pick up
// the same CRT post-processing (bloom, soften, phosphor tint) as everything
// else instead of looking like a flat overlay.
type ImagePass struct {
	prog  uint32
	vao   uint32
	cache map[*image.RGBA]uint32
}

func NewImagePass() (*ImagePass, error) {
	prog, err := linkProgram(imageVertSrc, imageFragSrc)
	if err != nil {
		return nil, fmt.Errorf("image program: %w", err)
	}
	quadVBO := newQuadVBO()
	var vao uint32
	gl.GenVertexArrays(1, &vao)
	gl.BindVertexArray(vao)
	gl.BindBuffer(gl.ARRAY_BUFFER, quadVBO)
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointerWithOffset(0, 2, gl.FLOAT, false, 2*4, 0)
	gl.BindVertexArray(0)
	return &ImagePass{prog: prog, vao: vao, cache: make(map[*image.RGBA]uint32)}, nil
}

// getTexture uploads img once and caches by pointer identity — a Screen
// Clone() copies the PlacedImage slice but keeps the same *image.RGBA, so
// this only re-uploads when a genuinely new image is placed.
func (p *ImagePass) getTexture(img *image.RGBA) uint32 {
	if tex, ok := p.cache[img]; ok {
		return tex
	}
	var tex uint32
	gl.GenTextures(1, &tex)
	gl.BindTexture(gl.TEXTURE_2D, tex)
	gl.PixelStorei(gl.UNPACK_ALIGNMENT, 1)
	b := img.Bounds()
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, int32(b.Dx()), int32(b.Dy()), 0, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(img.Pix))
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	gl.BindTexture(gl.TEXTURE_2D, 0)
	p.cache[img] = tex
	return tex
}

// Draw renders each placed image at cellW/cellH-scaled pixel coordinates
// into fbo.
func (p *ImagePass) Draw(fbo *FBO, images []screen.PlacedImage, cellW, cellH float32) {
	if len(images) == 0 {
		return
	}
	fbo.Bind()
	gl.Enable(gl.BLEND)
	gl.BlendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA)
	gl.UseProgram(p.prog)
	gl.BindVertexArray(p.vao)

	screenW, screenH := float32(fbo.W), float32(fbo.H)
	uScreen := gl.GetUniformLocation(p.prog, gl.Str("uScreenSize\x00"))
	uPos := gl.GetUniformLocation(p.prog, gl.Str("uPos\x00"))
	uSize := gl.GetUniformLocation(p.prog, gl.Str("uSize\x00"))
	uImage := gl.GetUniformLocation(p.prog, gl.Str("uImage\x00"))
	gl.Uniform2f(uScreen, screenW, screenH)
	gl.Uniform1i(uImage, 0)
	gl.ActiveTexture(gl.TEXTURE0)

	for _, pi := range images {
		gl.BindTexture(gl.TEXTURE_2D, p.getTexture(pi.Img))
		b := pi.Img.Bounds()
		gl.Uniform2f(uPos, float32(pi.Col)*cellW, float32(pi.Row)*cellH)
		gl.Uniform2f(uSize, float32(b.Dx()), float32(b.Dy()))
		gl.DrawArrays(gl.TRIANGLES, 0, 6)
	}

	gl.BindVertexArray(0)
	gl.Disable(gl.BLEND)
	fbo.Unbind()
}
