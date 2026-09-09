#version 330 core

layout(location = 0) in vec2 aPos;
layout(location = 1) in vec2 aCellPos;
layout(location = 2) in vec3 aColor;
layout(location = 3) in float aStyle;

uniform vec2 uCellSize;
uniform vec2 uScreenSize;
uniform vec2 uOffset;

out vec2 vLocal;  // 0..1 within the cell: x left..right, y top..bottom
out vec3 vColor;
out float vStyle;

// Draws one full-cell quad per underlined cell; cell_underline.frag places
// the actual decoration band (and, for curly, its wave) within it using
// vLocal — the underline never needs to shrink the instance quad itself,
// since every style it draws fits within the cell's own bounds.
void main() {
	vec2 pixelPos = uOffset + aCellPos + aPos * uCellSize;
	vec2 ndc = (pixelPos / uScreenSize) * 2.0 - 1.0;
	ndc.y = -ndc.y;
	gl_Position = vec4(ndc, 0.0, 1.0);
	vLocal = aPos;
	vColor = aColor;
	vStyle = aStyle;
}
