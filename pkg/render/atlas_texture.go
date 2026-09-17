package render

import (
	"github.com/go-gl/gl/v3.3-core/gl"

	"github.com/moozd/tubeless/pkg/font"
)

// uploadAtlas creates a single-channel GL texture from a font atlas. The
// atlas is rasterized larger than its on-screen cell size (see
// cmd/tubeless's atlasScale) so mipmapped minification — not the source
// rasterization — is what actually smooths glyph edges down to display
// size; plain LINEAR would alias/shimmer on a minification this large.
//
// MIN_FILTER is LINEAR_MIPMAP_NEAREST (bilinear within a single mip
// level), not the LINEAR_MIPMAP_LINEAR (trilinear, blending TWO
// adjacent levels) an unrelated 3D scene would want. Trilinear exists to
// hide POPPING as an object's distance — and so its true LOD — changes
// continuously frame to frame; nothing here ever does that; every cell
// in the grid is drawn at the exact same, fixed minification ratio
// every frame (cmd/tubeless's physicalCellSize rounds the display cell
// size specifically so this ratio is exact, not a wobble around some
// average). Blending two mip levels together when the real LOD never
// moves off one of them just softens the result for no benefit —
// visibly blurrier than sampling that single correct level directly,
// most noticeable at the shallower mip chains a standard-DPI display's
// lower atlas.scale base produces (see autoAtlasScale), where there are
// only one or two levels to begin with and trilinear's blend is a much
// bigger fraction of the final image.
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
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR_MIPMAP_NEAREST)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	gl.BindTexture(gl.TEXTURE_2D, 0)
	return tex
}
