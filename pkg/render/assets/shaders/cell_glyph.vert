#version 330 core

layout(location = 0) in vec2 aPos;
layout(location = 1) in vec2 aCellPos;
layout(location = 2) in vec2 aUVOffset;
layout(location = 3) in vec2 aUVSize;
layout(location = 4) in vec3 aColor;
layout(location = 5) in vec3 aBgColor;
layout(location = 6) in float aStyle;
layout(location = 7) in float aShear;
layout(location = 8) in float aWidthScale;

uniform vec2 uCellSize;
uniform vec2 uScreenSize;
uniform vec2 uOffset;

out vec2 vUV;
out vec3 vColor;
out vec3 vBgColor;
out float vStyle;

// Synthetic italic slant, in cell-widths of horizontal shift from the
// glyph's bottom edge to its top — used only for a style that has no
// real italic/bold-italic face (see CellPass.glyphStyle). Only the
// on-screen quad shears; vUV keeps sampling the atlas with the
// unsheared aPos, so the rasterizer slants the sampled bitmap along
// with the quad instead of distorting which texels get sampled.
const float italicSlant = 0.22;

void main() {
	vec2 p = aPos;
	p.x += (1.0 - p.y) * aShear * italicSlant;
	// aWidthScale stretches only the quad's on-screen extent, past 1
	// cell's worth of uCellSize, for a widened icon glyph (see
	// CellPass.iconCanWiden) — vUV below still samples with the
	// unstretched aPos, so it maps 1:1 onto that glyph's own
	// correspondingly-wider atlas rect (Atlas.WideGlyphs) instead of
	// stretching a normal single-cell glyph's image.
	vec2 size = uCellSize;
	size.x *= aWidthScale;
	vec2 pixelPos = uOffset + aCellPos + p * size;
	vec2 ndc = (pixelPos / uScreenSize) * 2.0 - 1.0;
	ndc.y = -ndc.y;
	gl_Position = vec4(ndc, 0.0, 1.0);
	vUV = aUVOffset + aPos * aUVSize;
	vColor = aColor;
	vBgColor = aBgColor;
	vStyle = aStyle;
}
