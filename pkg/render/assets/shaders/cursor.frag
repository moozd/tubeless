#version 330 core

in vec2 vUV;
out vec4 fragColor;

uniform vec2 uPos;        // top-left of the cursor cell, in pixels
uniform vec2 uSize;       // cell size, in pixels
uniform vec2 uScreenSize; // full window size, in pixels
uniform float uBright;    // breathing pulse, 0..1
uniform float uGlow;      // edge anti-aliasing width, in pixels
uniform vec3 uAccent;     // phosphor color for the cursor block
uniform float uMorph;     // 0 = at-rest shape, 1 = full ball+tail
uniform vec2 uTailDir;    // unit vector, screen space, pointing from the
                          // head's center toward the tail's far tip
uniform float uTailLen;   // tail length, in pixels (0 when not gliding fast)
uniform float uShape;     // 0 = block, 1 = bar, 2 = underline
uniform float uRadius;    // at-rest corner radius, as a fraction of the
                          // shape's own short half-dimension

uniform float uGlassMode;    // 0 = normal glow cursor, 1 = frosted glass
uniform sampler2D uScene;    // sharp scene, sampled only in glass mode
uniform float uGlassTint;    // 0..1, accent strength mixed into the sample
uniform float uGlassBlur;    // px, refraction sample spread
uniform float uGlassRefract; // px, outward bend of the sampled scene
uniform float uGlassOpacity; // 0..1, core opacity of the glass panel

// Bar/underline are thin strips pinned to one edge of the cell, sized as
// a fraction of it — the same proportions iTerm/Alacritty/Kitty use for
// their own I-beam/underline cursors.
const float BAR_WIDTH_FRAC = 0.18;
const float LINE_HEIGHT_FRAC = 0.16;

// Renders the terminal cursor as a sharp, slightly rounded phosphor shape
// at rest (block, bar, or underline — see uShape). As the CPU-side glide
// speeds up, uMorph rounds the shape into a full ball and streams a
// tapering, fading tail out behind it along uTailDir; both relax back into
// the plain shape as the glide slows, all driven by uMorph so the shape
// change itself reads as a morph rather than a cut. Its intensity is
// modulated by uBright throughout. The whole window is covered so pixels
// far from the cursor output black (or, in glass mode, fully transparent).
//
// In glass mode the cursor draws as a frosted macOS-style panel instead:
// the scene behind it is refracted and softly blurred, tinted toward
// uAccent, with a thin rim light and a top-left specular highlight. The
// caller drives uMorph/uBright to 0/1 (static, no ball/tail) whenever
// glass mode is on — see Renderer.RenderEffects — so this shader only
// has to render whatever those inputs say, the same as the normal path.
vec3 sampleScene(vec2 offsetPx, vec2 refractUV) {
	vec2 toUV = vec2(1.0 / uScreenSize.x, -1.0 / uScreenSize.y);
	return texture(uScene, vUV + refractUV + offsetPx * toUV).rgb;
}

void main() {
	// fullscreen.vert does not flip Y (unlike cell.vert): vUV.y = 0 is the
	// bottom of the window. uPos is top-origin (row 0 = top), so invert Y
	// before converting to pixel space.
	vec2 px = vec2(vUV.x, 1.0 - vUV.y) * uScreenSize;
	vec2 halfSize = uSize * 0.5;
	vec2 cellCenter = uPos + halfSize;

	// The at-rest box for the selected shape: block covers the whole
	// cell; bar is a thin strip on its left edge; underline a thin strip
	// on its bottom edge (px is top-origin here, so "bottom" is uPos.y +
	// uSize.y, the larger y).
	vec2 restHalf = halfSize;
	vec2 restCenter = cellCenter;
	if (uShape > 0.5 && uShape < 1.5) {
		float w = uSize.x * BAR_WIDTH_FRAC;
		restHalf = vec2(w * 0.5, halfSize.y);
		restCenter = vec2(uPos.x + w * 0.5, cellCenter.y);
	} else if (uShape >= 1.5) {
		float h = uSize.y * LINE_HEIGHT_FRAC;
		restHalf = vec2(halfSize.x, h * 0.5);
		restCenter = vec2(cellCenter.x, uPos.y + uSize.y - h * 0.5);
	}

	// The head morphs from the rounded-rect at-rest shape into a perfect
	// ball inscribed in it (a square half-extent equal to its own radius)
	// by interpolating both toward the shape's shorter half-dimension —
	// at uMorph == 1 that makes the rounded-rect SDF below degenerate
	// into an exact circle SDF, so the ball never outgrows the shape's
	// own footprint.
	float minDim = min(restHalf.x, restHalf.y);
	vec2 headHalf = mix(restHalf, vec2(minDim), uMorph);
	float headRadius = mix(minDim * uRadius, minDim, uMorph);
	vec2 center = restCenter;

	// Signed distance to the rounded rectangle: negative inside, 0 on the
	// boundary, positive outside. The min(max(q.x,q.y),0.0) term is the
	// interior branch — length(max(q,0.0)) alone is only correct outside
	// the box; without it every interior point (any q.x,q.y <= 0) evaluates
	// to exactly -headRadius regardless of how deep inside it is, which
	// happened to look right only because headRadius used to have a
	// hardcoded floor (comfortably negative); at uRadius == 0 (a square
	// cursor) that collapsed to headDist == 0 everywhere inside — right on
	// the antialiasing edge — rendering the cursor at zero brightness.
	vec2 q = abs(px - center) - (headHalf - headRadius);
	float headDist = min(max(q.x, q.y), 0.0) + length(max(q, 0.0)) - headRadius;
	float headCore = 1.0 - smoothstep(-uGlow, 0.0, headDist);

	// The tail is a round cone (a capsule tapering to a point) from the
	// head's center out to its tip, dimming toward the tip so it reads as
	// a streak rather than a hard-edged wedge. The segment projection's
	// divide is guarded with a safe denominator instead of a branch on
	// uTailLen == 0; step() zeroes the whole contribution back out for a
	// stationary cursor (uTailLen == 0, so the segment degenerates to a
	// point) rather than drawing a phantom dot at the head's own center.
	vec2 tip = center + uTailDir * uTailLen;
	vec2 seg = tip - center;
	float segLen2 = dot(seg, seg);
	vec2 toPx = px - center;
	float h = clamp(dot(toPx, seg) / max(segLen2, 1e-6), 0.0, 1.0);
	float tailRadius = mix(headRadius, headRadius * 0.08, h);
	float tailDist = length(toPx - seg * h) - tailRadius;
	float tailCore = (1.0 - smoothstep(-uGlow, 0.0, tailDist))
		* (1.0 - h * 0.85) * step(1e-6, segLen2);

	// Union of head and tail coverage, not a sum — they overlap near the
	// head, and adding would blow past 1.0 there.
	float core = max(headCore, tailCore);
	float v = clamp(core, 0.0, 1.0) * uBright;

	if (uGlassMode < 0.5) {
		fragColor = vec4(uAccent * v, 1.0);
		return;
	}

	// Frosted glass: bend the sample point outward from the shape's
	// center (a cheap stand-in for real refraction) and average a small
	// cross of taps around it for a soft blur, then tint toward the
	// accent color. Composited premultiplied — see inset.frag's uGlassMode
	// branch — so alpha (headCore * uGlassOpacity * uBright) is baked
	// into the output color.
	vec2 normal = length(toPx) > 1e-4 ? normalize(toPx) : vec2(0.0);
	vec2 refractUV = normal * uGlassRefract * vec2(1.0 / uScreenSize.x, -1.0 / uScreenSize.y);

	vec3 scene = sampleScene(vec2(0.0), refractUV);
	if (uGlassBlur > 0.0) {
		scene += sampleScene(vec2(uGlassBlur, 0.0), refractUV);
		scene += sampleScene(vec2(-uGlassBlur, 0.0), refractUV);
		scene += sampleScene(vec2(0.0, uGlassBlur), refractUV);
		scene += sampleScene(vec2(0.0, -uGlassBlur), refractUV);
		scene *= 0.2;
	}

	vec3 glass = mix(scene, uAccent, uGlassTint);
	float rim = 1.0 - smoothstep(0.0, uGlow + 1.5, abs(headDist));
	glass += rim * 0.3;
	float highlight = clamp(dot(normal, normalize(vec2(-1.0, 1.0))), 0.0, 1.0);
	glass += highlight * 0.18 * headCore;

	float alpha = headCore * uGlassOpacity * uBright;
	fragColor = vec4(glass * alpha, alpha);
}
