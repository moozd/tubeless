#version 330 core

layout(location = 0) in vec2 aPos;
layout(location = 1) in vec2 aCellPos;
layout(location = 2) in vec2 aRectOffset; // sub-rect offset within the cell, 0..1
layout(location = 3) in vec2 aRectSize;   // sub-rect size within the cell, 0..1
layout(location = 4) in vec3 aColor;
layout(location = 5) in vec4 aRadius;     // corner radii in pixels: TL, TR, BR, BL

uniform vec2 uCellSize;
uniform vec2 uScreenSize;
uniform vec2 uOffset;

out vec2 vLocal;    // fragment position relative to the rect center, pixels
out vec2 vHalfSize;  // rect half-size, pixels
out vec3 vColor;
out vec4 vRadius;

// Draws a background fill or a single-rect block glyph (█▀▄▌▐ etc.) as a
// procedural rect instead of a sampled texture, so cell_rect.frag can round
// its corners against real neighbor cells (see cellpass.go's cornerRadii).
// aRectOffset/aRectSize place the rect within the cell — (0,0)-(1,1) for a
// full-cell background, a sub-rect fraction for a block glyph.
void main() {
	vec2 rectPx = aRectSize * uCellSize;
	vec2 originPx = uOffset + aCellPos + aRectOffset * uCellSize;
	vec2 pixelPos = originPx + aPos * rectPx;
	vec2 ndc = (pixelPos / uScreenSize) * 2.0 - 1.0;
	ndc.y = -ndc.y;
	gl_Position = vec4(ndc, 0.0, 1.0);

	vHalfSize = rectPx * 0.5;
	vLocal = (aPos - 0.5) * rectPx;
	vColor = aColor;
	vRadius = aRadius;
}
