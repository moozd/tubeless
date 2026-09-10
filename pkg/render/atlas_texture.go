package render

import (
	"github.com/go-gl/gl/v3.3-core/gl"

	"github.com/moozd/tubeless/pkg/font"
)

// uploadAtlas creates a single-channel GL texture from a font atlas. The
// atlas is rasterized larger than its on-screen cell size (see
// cmd/tubeless's atlasScale) so mipmapped trilinear minification — not the
// source rasterization — is what actually smooths glyph edges down to
// display size; plain LINEAR would alias/shimmer on a minification this
// large.
//
// uploadAtlas is also what CellPass.newAtlasTextures calls per style — a
// nil atlas (no real Italic/BoldItalic face was loaded) is never passed
// here; callers alias that slot's texture id to a sibling style instead
// (see newAtlasTextures), since aStyle routing guarantees it's never
// sampled.
func uploadAtlas(atlas *font.Atlas) uint32 {
	var tex uint32
	gl.GenTextures(1, &tex)
	gl.BindTexture(gl.TEXTURE_2D, tex)
	gl.PixelStorei(gl.UNPACK_ALIGNMENT, 1)
	bounds := atlas.Image.Bounds()
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.R8, int32(bounds.Dx()), int32(bounds.Dy()),
		0, gl.RED, gl.UNSIGNED_BYTE, gl.Ptr(atlas.Image.Pix))
	gl.GenerateMipmap(gl.TEXTURE_2D)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR_MIPMAP_LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	gl.BindTexture(gl.TEXTURE_2D, 0)
	return tex
}
