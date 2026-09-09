#version 330 core

in vec2 vUV;
out vec4 fragColor;

uniform sampler2D uScene;
uniform vec2 uTexel;     // {1/width, 1/height} of the source
uniform float uSigma;    // gaussian sigma, in texels (the config blur radius)
uniform float uSamples;  // taps each side of center (chosen on the CPU)
uniform vec2 uDir;       // (1,0) for the horizontal pass, (0,1) vertical
uniform float uStrength; // 0..1 glow intensity soft-added over the original
uniform sampler2D uOrig; // the un-blurred source (only used for the mix)
uniform float uFinal;    // 1.0 on the vertical pass (apply the soft-add), 0.0 on the horizontal pass (pure blur)

// Separable gaussian bloom over the line-art layer (box-drawing and
// powerline glyphs only — solid blocks and backgrounds get true geometric
// rounding instead, see cell_rect.frag, and are composited underneath
// this layer rather than through it). The layer is transparent except
// where a glyph actually drew, so this blurs both color and coverage
// (alpha) and the result is later alpha-composited over the scene rather
// than replacing it — see CopyPass.DrawOver.
//
// The blurred pass is soft-added over the sharp original (same technique
// as the cursor glow in inset.frag) rather than mixed over it: a straight
// mix(orig, blurred, uStrength) dims a thin line's core along with its
// edges, eroding it toward invisibility at any real strength. Soft-adding
// keeps the sharp core at full brightness/coverage and only lets the glow
// bleed into the transparent pixels around it — a rounded, glowing edge
// instead of a flat smear. Not premultiplied before blurring — the glow's
// hue softens slightly as it fades into full transparency, which reads as
// part of the bloom rather than as a defect.
void main() {
	float sigma = max(uSigma, 0.5);
	vec2 dir = uDir * uTexel;

	vec4 acc = texture(uScene, vUV);
	float sum = 1.0;
	for (int i = 1; i <= 16; i++) {
		if (float(i) > uSamples) {
			break;
		}
		float w = exp(-0.5 * float(i * i) / (sigma * sigma));
		vec2 off = dir * float(i);
		acc += (texture(uScene, vUV + off) + texture(uScene, vUV - off)) * w;
		sum += 2.0 * w;
	}

	vec4 blurred = acc / sum;
	if (uFinal < 0.5) {
		// Intermediate horizontal pass: a separable blur needs this to stay
		// a pure blur of uScene, or the vertical pass below would blur an
		// already-glow-boosted image and re-apply the glow on top of that.
		fragColor = blurred;
		return;
	}

	vec4 orig = texture(uOrig, vUV);
	vec4 glow = blurred * uStrength;
	vec3 rgb = orig.rgb + glow.rgb * (1.0 - orig.a);
	float a = clamp(orig.a + glow.a * (1.0 - orig.a), 0.0, 1.0);
	fragColor = vec4(rgb, a);
}
