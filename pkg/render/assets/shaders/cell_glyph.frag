#version 330 core

in vec2 vUV;
in vec3 vColor;
in vec3 vBgColor;
out vec4 fragColor;

uniform sampler2D uAtlas;

// Ported from Ghostty's cell_text.f.glsl "linear-corrected" mode (its
// default everywhere but macOS). We always render into an sRGB-capable
// target (see window.go's GL_FRAMEBUFFER_SRGB), so the GPU blends in
// linear light — but naive linear blending alone makes light-on-dark
// text look thicker and dark-on-light text thinner than gamma-space
// blending does, which is what decades of font hinting assume. This
// computes a corrected alpha so the linear blend lands on the same
// perceived weight gamma-space blending would have produced, while
// still avoiding the hue-shift/darkening artifacts naive gamma blending
// causes on saturated color pairs.
float luminance(vec3 c) {
	return dot(c, vec3(0.2126, 0.7152, 0.0722));
}

float unlinearize(float v) {
	return v <= 0.0031308 ? v * 12.92 : pow(v, 1.0 / 2.4) * 1.055 - 0.055;
}

float linearize(float v) {
	return v <= 0.04045 ? v / 12.92 : pow((v + 0.055) / 1.055, 2.4);
}

void main() {
	float a = texture(uAtlas, vUV).r;

	float fgL = luminance(vColor);
	float bgL = luminance(vBgColor);
	if (abs(fgL - bgL) > 0.001) {
		float blendL = linearize(unlinearize(fgL) * a + unlinearize(bgL) * (1.0 - a));
		a = clamp((blendL - bgL) / (fgL - bgL), 0.0, 1.0);
	}

	// Premultiplied output (paired with a GL_ONE/GL_ONE_MINUS_SRC_ALPHA
	// blend, see cellpass.go): straight alpha (vColor, a) blended with the
	// usual GL_SRC_ALPHA/GL_ONE_MINUS_SRC_ALPHA factors is only correct
	// over an opaque destination. DrawLineArt's target starts fully
	// transparent, where that blend squares the alpha on every overlapping
	// draw (dst.a starts at 0, so the alpha channel's own SRC_ALPHA factor
	// multiplies a by itself) and reads as dim/washed-out line art.
	// Premultiplied blending is correct over both transparent and opaque
	// destinations, so this also just works, unchanged, for DrawText.
	fragColor = vec4(vColor * a, a);
}
