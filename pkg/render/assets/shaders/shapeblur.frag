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
uniform float uGlowOnly; // 1.0 outputs just the halo (see BlurPass.DrawGlow), 0.0 soft-adds it over uOrig

// Separable gaussian image-space bloom over a literal, non-text effect
// source. The terminal scene is drawn separately; this shader only sees the
// block/border pixels selected by the renderer, so it never infers surfaces
// from terminal cells or changes how escape-sequence output is interpreted.
//
// The final pass screen-blends the blurred image over the original. A
// straight mix would dim sharp strokes; screen-style soft-add preserves the
// original core and only lifts darker neighboring pixels into a glow.
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

	if (uGlowOnly > 0.5) {
		// Halo only, alpha 0: under a premultiplied ONE/ONE_MINUS_SRC_ALPHA
		// blend (CopyPass.DrawOver) that is a pure additive light, so the
		// glow brightens whatever sits beneath it without covering it.
		fragColor = vec4(blurred.rgb * uStrength, 0.0);
		return;
	}

	vec4 orig = texture(uOrig, vUV);
	vec3 glow = blurred.rgb * uStrength;
	vec3 rgb = orig.rgb + glow * (1.0 - orig.rgb);
	fragColor = vec4(clamp(rgb, 0.0, 1.0), orig.a);
}
