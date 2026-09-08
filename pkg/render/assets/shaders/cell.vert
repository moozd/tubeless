#version 330 core

layout(location = 0) in vec2 aPos;
layout(location = 1) in vec2 aCellPos;
layout(location = 2) in vec2 aUVOffset;
layout(location = 3) in vec2 aUVSize;
layout(location = 4) in vec3 aColor;

uniform vec2 uCellSize;
uniform vec2 uScreenSize;
uniform vec2 uOffset;

out vec2 vUV;
out vec3 vColor;

void main() {
	vec2 pixelPos = uOffset + aCellPos + aPos * uCellSize;
	vec2 ndc = (pixelPos / uScreenSize) * 2.0 - 1.0;
	ndc.y = -ndc.y;
	gl_Position = vec4(ndc, 0.0, 1.0);
	vUV = aUVOffset + aPos * aUVSize;
	vColor = aColor;
}
