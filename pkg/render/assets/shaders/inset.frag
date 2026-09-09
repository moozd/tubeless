#version 330 core

in vec2 vUV;
out vec4 fragColor;

uniform sampler2D uScene;      // sharp scene (blurred shapes + sharp text)
uniform sampler2D uCursor;     // animated cursor glow
uniform vec3 uAccent;          // phosphor color (linear) used as the tint hue
uniform float uBgTint;         // empty-screen brightness, 0..1
uniform float uInsetShadow;    // strength of the radial tube-face falloff, 0..1
uniform float uAspect;         // screen width / height

// Final composite. The cursor is soft-added over the scene — cursor *
// (1 - scene) adds glow where the screen is dark and barely lifts bright
// pixels, so it reads as a beam sweeping over the text underneath.
//
// Then the flat render becomes a recessed tube face. The screen is never
// dead black: empty areas read as a very dim phosphor glow (uAccent *
// uBgTint).
//
// The inset is a single continuous radial falloff — distance from the
// screen center, normalized so the corners (not the edges) are the darkest
// point, and squashed by the aspect ratio so it stays elliptical on any
// window shape. That is how a curved tube face actually shades: brightest
// center, smooth physical falloff toward a darkened rim, corners deepest.
// No flat per-edge band, which reads as a drawn frame rather than glass.
//
// GL_DITHER is disabled (see window.go) because the driver's pattern was
// visible as fixed noise over the wide smooth gradients this pipeline draws;
// that leaves the 8-bit sRGB write quantization free to posterize the very
// same gradients into faint steps. This pass therefore adds its own tiny,
// temporally-stable ordered dither (a 4x4 Bayer matrix, sub-sRGB-step in
// size) before the hardware's sRGB encode, which trades the invisible steps
// for an invisible half-step of pattern noise instead.
float bayer4(vec2 p) {
	ivec2 i = ivec2(mod(floor(p), 4.0));
	int idx = i.y * 4 + i.x;
	const int m[16] = int[16](
		 0,  8,  2, 10,
		12,  4, 14,  6,
		 3, 11,  1,  9,
		15,  7, 13,  5
	);
	return m[idx] / 16.0;
}

void main() {
	vec3 scene = texture(uScene, vUV).rgb;
	vec3 cursor = texture(uCursor, vUV).rgb;
	vec3 glow = scene + cursor * (1.0 - scene);

	vec3 color = glow + uAccent * uBgTint;

	vec2 p = (vUV - 0.5) * 2.0;
	p.x *= uAspect;
	float d = clamp(length(p) / length(vec2(uAspect, 1.0)), 0.0, 1.0);
	float shade = 1.0 - uInsetShadow * pow(d, 2.4);
	color *= shade;

	// One quarter of an 8-bit step, centered; smooth gradients land between
	// two representable values and the pattern biases each pixel toward the
	// nearer one instead of all snapping to the same quantization floor.
	color += (bayer4(gl_FragCoord.xy) - 0.5) * (1.0 / 255.0) * 0.5;

	fragColor = vec4(clamp(color, 0.0, 1.0), 1.0);
}
