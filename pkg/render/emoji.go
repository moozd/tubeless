package render

import (
	"image"
	"log"
	"strings"

	"github.com/go-gl/gl/v3.3-core/gl"

	"github.com/moozd/tubeless/pkg/font"
	"github.com/moozd/tubeless/pkg/screen"
)

// emojiCell is one color emoji to draw: its bitmap, the grid cell it
// starts at, and how many columns (1 or 2) it spans.
type emojiCell struct {
	img   *image.RGBA
	x, y  int
	cells int
}

// emojiCache rasterizes each emoji cluster from the system color emoji
// font the first time a frame shows it, and keeps the result — a miss
// too (nil), so a cluster the font lacks is looked up only once. The
// font itself is opened on the first emoji, not at startup.
type emojiCache struct {
	pixelHeight int
	face        *font.EmojiFace
	isDisabled  bool
	images      map[string]*image.RGBA
}

func newEmojiCache(pixelHeight int) *emojiCache {
	return &emojiCache{pixelHeight: pixelHeight, images: make(map[string]*image.RGBA)}
}

// get returns cluster's bitmap, or false when there is no color glyph.
func (c *emojiCache) get(cluster string) (*image.RGBA, bool) {
	if img, isSeen := c.images[cluster]; isSeen {
		return img, img != nil
	}
	if !c.open() {
		return nil, false
	}
	img, ok := c.face.Render(cluster)
	if !ok {
		img = nil
	}
	c.images[cluster] = img
	return img, ok
}

// open loads the emoji font once. Without one, emoji fall back to the
// glyph atlas, the same as before color emoji existed.
func (c *emojiCache) open() bool {
	if c.face != nil {
		return true
	}
	if c.isDisabled {
		return false
	}
	path, err := font.FindEmojiFont()
	if err == nil {
		c.face, err = font.OpenEmojiFace(path, c.pixelHeight)
	}
	if err != nil {
		log.Printf("color emoji disabled: %v", err)
		c.isDisabled = true
		return false
	}
	return true
}

// appendEmoji queues the cell at (x,y) for DrawEmoji when the emoji
// font has a color glyph for it, and reports whether it did.
func (cp *CellPass) appendEmoji(grid [][]screen.Cell, cols, x, y int) bool {
	img, ok := cp.emoji.get(grid[y][x].Text())
	if !ok {
		return false
	}
	e := emojiCell{img: img, x: x, y: y, cells: emojiSpan(grid, cols, x, y)}
	cp.emojiScratch = append(cp.emojiScratch, e)
	return true
}

// emojiSpan is how many columns the cell at (x,y) covers: 2 when a
// WideSpacer follows it, else 1.
func emojiSpan(grid [][]screen.Cell, cols, x, y int) int {
	if x+1 < cols && grid[y][x+1].Rune == screen.WideSpacer {
		return 2
	}
	return 1
}

// isEmojiCandidate reports whether the cell at (x,y) should try the
// color emoji font before the glyph atlas: a wide cell (screen gives
// emoji 2 columns) or one that asks for emoji presentation with VS16.
// Nerd Font icons are never wide here, so they never reach the emoji
// font.
func isEmojiCandidate(grid [][]screen.Cell, cols, x, y int) bool {
	cell := grid[y][x]
	if strings.Contains(cell.Tail, "️") {
		return true
	}
	return emojiSpan(grid, cols, x, y) == 2 && !font.IsPrivateUseRune(cell.Rune)
}

// emojiQuad fits a square emoji into its cells: as tall as the cell,
// narrower if the cells are, centered both ways.
func emojiQuad(e emojiCell, cw, ch float32) (x, y, size float32) {
	boxW := float32(e.cells) * cw
	size = min(boxW, ch)
	x = float32(e.x)*cw + (boxW-size)/2
	y = float32(e.y)*ch + (ch-size)/2
	return x, y, size
}

// DrawEmoji paints each emoji bitmap over its cells, after the text.
// offsetX/offsetY is the grid's own offset in fbo (see GridOffset). The
// bitmaps are premultiplied sRGB, so they upload as SRGB8_ALPHA8 (the
// scene is linear light) and blend with GL_ONE.
func (p *ImagePass) DrawEmoji(fbo *FBO, emoji []emojiCell, cw, ch, offsetX, offsetY float32) {
	if len(emoji) == 0 {
		return
	}
	fbo.Bind()
	gl.Enable(gl.BLEND)
	gl.BlendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA)
	gl.UseProgram(p.prog)
	gl.BindVertexArray(p.vao)
	gl.Uniform2f(gl.GetUniformLocation(p.prog, gl.Str("uScreenSize\x00")), float32(fbo.W), float32(fbo.H))
	gl.Uniform1i(gl.GetUniformLocation(p.prog, gl.Str("uImage\x00")), 0)
	gl.Uniform2f(gl.GetUniformLocation(p.prog, gl.Str("uUVPos\x00")), 0, 0)
	gl.Uniform2f(gl.GetUniformLocation(p.prog, gl.Str("uUVSize\x00")), 1, 1)
	uPos := gl.GetUniformLocation(p.prog, gl.Str("uPos\x00"))
	uSize := gl.GetUniformLocation(p.prog, gl.Str("uSize\x00"))
	gl.ActiveTexture(gl.TEXTURE0)

	for _, e := range emoji {
		gl.BindTexture(gl.TEXTURE_2D, p.getEmojiTexture(e.img))
		x, y, size := emojiQuad(e, cw, ch)
		gl.Uniform2f(uPos, offsetX+x, offsetY+y)
		gl.Uniform2f(uSize, size, size)
		gl.DrawArrays(gl.TRIANGLES, 0, 6)
	}

	gl.BindVertexArray(0)
	gl.Disable(gl.BLEND)
	fbo.Unbind()
}

// getEmojiTexture uploads img once, mipmapped: a 160px strike drawn into
// a ~30px cell needs the mip chain to minify without aliasing.
func (p *ImagePass) getEmojiTexture(img *image.RGBA) uint32 {
	if tex, ok := p.emojiCache[img]; ok {
		return tex
	}
	var tex uint32
	gl.GenTextures(1, &tex)
	gl.BindTexture(gl.TEXTURE_2D, tex)
	gl.PixelStorei(gl.UNPACK_ALIGNMENT, 1)
	b := img.Bounds()
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.SRGB8_ALPHA8, int32(b.Dx()), int32(b.Dy()), 0, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(img.Pix))
	gl.GenerateMipmap(gl.TEXTURE_2D)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR_MIPMAP_LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	gl.BindTexture(gl.TEXTURE_2D, 0)
	p.emojiCache[img] = tex
	return tex
}
