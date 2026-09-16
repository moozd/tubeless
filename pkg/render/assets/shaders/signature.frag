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
// never blended across a row/column boundary. uRangeLo/uRangeHi restrict
// the reduced axis to a sub-range of the source texture (in its own 0..1
// UV space) instead of the whole thing — see imagediff.go's per-band
// detection, which uses this to average only a split window's own pane
// (a column sub-range for a row signature, a row sub-range for a column
// signature) instead of bleeding in a frozen neighboring pane.
uniform float uSigLen;
uniform float uTaps;
uniform float uAxis;
uniform float uRangeLo;
uniform float uRangeHi;

void main() {
	float sliceLen = (uRangeHi - uRangeLo) / uSigLen;
	vec3 sum = vec3(0.0);
	if (uAxis < 0.5) {
		// cell_rect.vert/cell_glyph.vert negate NDC y so grid row 0 lands
		// at the top of the window — meaning row 0 is rendered at the
		// framebuffer's OpenGL-convention top, i.e. vUV.y close to 1, not
		// 0. Undoing that here (1.0 - vUV.y) is what makes output row
		// `oy` actually correspond to grid row `oy`; without it every row
		// signature is read from its mirror position, and DetectShift
		// ends up comparing entirely the wrong pairs of rows.
		float srcY = 1.0 - vUV.y;
		float sliceIndex = floor(vUV.x * uSigLen);
		float sliceStart = uRangeLo + sliceIndex * sliceLen;
		for (float i = 0.0; i < 64.0; i += 1.0) {
			if (i >= uTaps) break;
			float u = sliceStart + (i + 0.5) / uTaps * sliceLen;
			sum += texture(uScene, vec2(u, srcY)).rgb;
		}
	} else {
		float sliceIndex = floor(vUV.y * uSigLen);
		float sliceStart = uRangeLo + sliceIndex * sliceLen;
		for (float i = 0.0; i < 64.0; i += 1.0) {
			if (i >= uTaps) break;
			float v = sliceStart + (i + 0.5) / uTaps * sliceLen;
			sum += texture(uScene, vec2(vUV.x, v)).rgb;
		}
	}
	fragColor = vec4(sum / uTaps, 1.0);
}
