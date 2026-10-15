#version 330 core

in vec2 vUV;
out vec4 fragColor;

uniform sampler2D uScene;      // sharp scene (blurred shapes + sharp text)
uniform sampler2D uCursor;     // animated cursor glow (or glass panel)
uniform float uGlassMode;      // 0 = cursor is an additive glow, 1 = a
                                // premultiplied-alpha glass panel (see
                                // cursor.frag's own uGlassMode branch)
uniform vec3 uAccent;          // phosphor color (linear) used as the tint hue
uniform float uBgTint;         // empty-screen brightness, 0..1
uniform vec3 uAmbient;         // room light the unlit tube reflects (linear)
uniform float uInsetShadow;    // strength of the radial tube-face falloff, 0..1
uniform float uAspect;         // screen width / height
uniform float uTime;           // wrapped elapsed seconds, for noise/flicker

// Every uniform below is 0 (or Intensity/Amount 0) when its CRT effect is
// disabled in config — the default for all of them. Each is gated by an
// `if (u... > 0.0)` branch; since the condition is uniform across the
// whole draw call, a disabled effect costs nothing (no warp divergence).
uniform float uCurvature;               // barrel-distortion amount, 0 = flat
uniform float uAberration;              // chromatic aberration amount, UV units
uniform float uScanIntensity, uScanPeriod;
uniform float uMaskIntensity, uMaskCellSize;
uniform float uGridIntensity, uGridCellSize, uGridGap;
uniform float uNoiseIntensity;
uniform float uFlickerAmount, uFlickerSpeed;

// Final composite. Normally the cursor is soft-added over the scene —
// cursor * (1 - scene) adds glow where the screen is dark and barely
// lifts bright pixels, so it reads as a beam sweeping over the text
// underneath. In glass mode (uGlassMode > 0) cursor.frag instead hands
// back a premultiplied-alpha glass panel, composited with the standard
// "over" operator (cursor + scene * (1 - cursorAlpha)) so the panel
// reads as translucent rather than as more additive glow.
//
// Then the flat render becomes a recessed tube face. The screen is never
// dead black: empty areas read as a very dim phosphor glow (uAccent *
// uBgTint), over the room light the pale phosphor coating reflects
// back through the glass (uAmbient). That reflection is not emitted by
// the beam, so it is added after the scanline, shadow-mask and pixel-
// grid gaps: those darken only the light the tube makes itself.
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

float hash(vec2 p) {
	return fract(sin(dot(p, vec2(12.9898, 78.233))) * 43758.5453);
}

// curveUV bends uv (0..1) toward a barrel-distorted tube face: points near
// the center are nearly unmoved, points toward the corners bow outward.
// The result can leave [0,1]; tubeEdge masks that part to the black bezel.
vec2 curveUV(vec2 uv, float amount) {
	vec2 c = uv * 2.0 - 1.0;
	c += c * (c.yx * c.yx) * amount;
	return c * 0.5 + 0.5;
}

// TUBE_CORNER is the tube face's corner radius, as a fraction of the
// screen height: a real CRT's glass has visibly rounded corners.
const float TUBE_CORNER = 0.035;

// tubeEdge is how much of the curved tube face covers this pixel, 1 in
// the face and 0 on the bezel. It is a rounded-rectangle distance in
// aspect-correct units, anti-aliased over one pixel (fwidth), so the
// bowed edge is smooth instead of a stair-stepped hard cut.
float tubeEdge(vec2 uv) {
	vec2 halfSize = vec2(0.5 * uAspect, 0.5);
	vec2 c = (uv - 0.5) * vec2(uAspect, 1.0);
	vec2 q = abs(c) - halfSize + TUBE_CORNER;
	float sd = length(max(q, 0.0)) + min(max(q.x, q.y), 0.0) - TUBE_CORNER;
	float aa = max(fwidth(sd), 1e-5);
	return clamp(0.5 - sd / aa, 0.0, 1.0);
}

void main() {
	vec2 uv = vUV;
	float edge = 1.0;
	if (uCurvature > 0.0) {
		uv = curveUV(uv, uCurvature);
		edge = tubeEdge(uv);
		if (edge <= 0.0) {
			fragColor = vec4(0.0, 0.0, 0.0, 1.0);
			return;
		}
		uv = clamp(uv, 0.0, 1.0);
	}

	vec3 scene;
	if (uAberration > 0.0) {
		vec2 dir = uv - 0.5;
		scene = vec3(
			texture(uScene, uv - dir * uAberration).r,
			texture(uScene, uv).g,
			texture(uScene, uv + dir * uAberration).b
		);
	} else {
		scene = texture(uScene, uv).rgb;
	}
	vec4 cursorSample = texture(uCursor, uv);
	vec3 glow;
	if (uGlassMode > 0.5) {
		glow = cursorSample.rgb + scene * (1.0 - cursorSample.a);
	} else {
		glow = scene + cursorSample.rgb * (1.0 - scene);
	}

	vec3 color = glow + uAccent * uBgTint;

	// Vignette stays keyed on the original (un-warped) vUV — it's a
	// separate cosmetic falloff, not meant to compound with curvature's
	// own geometric bowing.
	vec2 p = (vUV - 0.5) * 2.0;
	p.x *= uAspect;
	float d = clamp(length(p) / length(vec2(uAspect, 1.0)), 0.0, 1.0);
	float shade = 1.0 - uInsetShadow * pow(d, 2.4);
	color *= shade;
	vec3 ambient = uAmbient * shade;

	if (uScanIntensity > 0.0) {
		float scan = 0.5 + 0.5 * sin(gl_FragCoord.y / uScanPeriod * 6.2831853);
		color *= 1.0 - uScanIntensity * scan;
	}

	if (uMaskIntensity > 0.0) {
		float col = mod(floor(gl_FragCoord.x / max(uMaskCellSize, 1.0) * 3.0), 3.0);
		vec3 maskColor = col < 0.5 ? vec3(1.0, 0.55, 0.55)
			: col < 1.5 ? vec3(0.55, 1.0, 0.55)
			: vec3(0.55, 0.55, 1.0);
		color *= mix(vec3(1.0), maskColor, uMaskIntensity);
	}

	if (uGridIntensity > 0.0) {
		vec2 cell = fract(gl_FragCoord.xy / max(uGridCellSize, 2.0));
		float isGap = max(step(cell.x, uGridGap), step(cell.y, uGridGap));
		color *= 1.0 - uGridIntensity * isGap;
	}

	color += ambient;

	// Signal noise rides on the video voltage, which drives the tube in
	// its gamma-encoded domain, so it is added there. Added in linear
	// light, the same amount is a huge step near black and turns an empty
	// screen into coarse grain.
	if (uNoiseIntensity > 0.0) {
		vec3 encoded = pow(max(color, 0.0), vec3(1.0 / 2.2));
		encoded += (hash(gl_FragCoord.xy + uTime * 1000.0) - 0.5) * uNoiseIntensity;
		color = pow(max(encoded, 0.0), vec3(2.2));
	}

	if (uFlickerAmount > 0.0) {
		float n = hash(vec2(floor(uTime * uFlickerSpeed), 0.0));
		color *= 1.0 - uFlickerAmount * abs(n - 0.5) * 2.0;
	}

	// One quarter of an 8-bit step, centered; smooth gradients land between
	// two representable values and the pattern biases each pixel toward the
	// nearer one instead of all snapping to the same quantization floor.
	color += (bayer4(gl_FragCoord.xy) - 0.5) * (1.0 / 255.0) * 0.5;
	color *= edge;

	fragColor = vec4(clamp(color, 0.0, 1.0), 1.0);
}
