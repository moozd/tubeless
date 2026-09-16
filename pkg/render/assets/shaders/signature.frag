#version 330 core

in vec2 vUV;
out vec4 fragColor;

uniform sampler2D uScene;
// uSigLen is the sample count along the axis being reduced (the output
// texture's width for a row signature, its height for a column
// signature) — see SignaturePass. uTaps is how many evenly-spaced points
// within each signature slice are averaged. uAxis picks the reduced axis:
// 0 reduces X (row signature, one fixed Y per output row), 1 reduces Y
// (column signature, one fixed X per output column) — see
// SignaturePass.draw's doc for why the preserved axis is a single sample,
// never blended across a row/column boundary.
uniform float uSigLen;
uniform float uTaps;
uniform float uAxis;

void main() {
	float sliceLen = 1.0 / uSigLen;
	vec3 sum = vec3(0.0);
	if (uAxis < 0.5) {
		float sliceStart = vUV.x - 0.5 * sliceLen;
		for (float i = 0.0; i < 64.0; i += 1.0) {
			if (i >= uTaps) break;
			float u = sliceStart + (i + 0.5) / uTaps * sliceLen;
			sum += texture(uScene, vec2(u, vUV.y)).rgb;
		}
	} else {
		float sliceStart = vUV.y - 0.5 * sliceLen;
		for (float i = 0.0; i < 64.0; i += 1.0) {
			if (i >= uTaps) break;
			float v = sliceStart + (i + 0.5) / uTaps * sliceLen;
			sum += texture(uScene, vec2(vUV.x, v)).rgb;
		}
	}
	fragColor = vec4(sum / uTaps, 1.0);
}
