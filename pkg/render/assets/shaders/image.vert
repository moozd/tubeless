#version 330 core

layout(location = 0) in vec2 aPos;

uniform vec2 uPos;
uniform vec2 uSize;
uniform vec2 uScreenSize;

out vec2 vUV;

void main() {
	vec2 pixelPos = uPos + aPos * uSize;
	vec2 ndc = (pixelPos / uScreenSize) * 2.0 - 1.0;
	ndc.y = -ndc.y;
	gl_Position = vec4(ndc, 0.0, 1.0);
	vUV = aPos;
}
