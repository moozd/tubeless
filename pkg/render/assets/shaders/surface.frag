#version 330 core

in vec2 vUV;
out vec4 fragColor;

uniform sampler2D uScene;
uniform vec2 uTexel;
uniform float uRadius;
uniform float uGradient; // 0..1 top-lit/bottom-shaded sheen strength

// edgeSignal is a continuous 0..1 "how much this sample stops being the
// base fill" value: 0 for the same opaque color as base, 1 for fully
// transparent or a clearly different color. cell_rect.frag antialiases
// both straight edges and cell-to-cell seams with real fractional alpha
// (see its own doc comment), so this varies smoothly across the true edge
// instead of stepping — edgeDist below interpolates against that sub-pixel
// ramp instead of snapping the corner to whole-texel rings, which is what
// read as pixelated at small radii.
float edgeSignal(vec3 base, vec2 uv) {
	vec4 s = texture(uScene, uv);
	vec3 rgb = s.a > 0.001 ? s.rgb / s.a : vec3(0.0);
	float colorDiff = clamp(distance(rgb, base) / 0.15, 0.0, 1.0);
	return max(1.0 - s.a, colorDiff);
}

const float EDGE_T = 0.5;
const float MAX_REACH = 16.0;

// Sub-pixel distance to the nearest edge along dir, up to radius texels.
// Walks whole texels to bracket the crossing, then linearly interpolates
// within that texel using the actual signal values on either side — exact
// for the linear ramp bilinear-filtered antialiased source pixels give.
float edgeDist(vec3 base, vec2 dir, float radius) {
	float prev = 0.0;
	for (int i = 1; i <= 17; i++) {
		float f = float(i);
		float cur = edgeSignal(base, vUV + dir * uTexel * f);
		if (cur >= EDGE_T) {
			float t = clamp((EDGE_T - prev) / max(cur - prev, 1e-4), 0.0, 1.0);
			return min(f - 1.0 + t, radius);
		}
		if (f >= radius) {
			break;
		}
		prev = cur;
	}
	return radius;
}

// Deliberately near a hard step rather than a soft blur: cornerCoverage
// below already supersamples this over a grid of sub-pixel offsets, so the
// antialiasing comes from averaging many near-binary samples (like MSAA),
// not from blurring any one of them. That's what keeps the curve crisp —
// widening this would soften the edge without adding any more roundness.
const float AA_WIDTH = 0.18;

float cornerKeep(float a, float b, float radius) {
	if (a >= radius || b >= radius) {
		return 1.0;
	}
	float d = length(vec2(radius - a, radius - b));
	return 1.0 - smoothstep(radius - AA_WIDTH, radius + AA_WIDTH, d);
}

// left+right (and up+down) sum to the shape's true local width (height) at
// this pixel only when both actually found their edge — each is an exact
// distance to its own edge in that case, wherever within the span the
// pixel sits. Box-drawing borders are only a stroke-width thin, far under
// most configured radii, so clamping the radius to that local thickness
// (mirroring cell_rect.frag's min(r, halfSize) clamp for filled rects)
// keeps a thin stroke's corner glyph intact instead of the fixed radius
// carving into it as if it belonged to a big filled panel. But edgeDist
// returns its search cap, not a real edge, once a block is wider than
// twice that cap — treating that as the true width shrank every corner's
// effective radius on any block bigger than ~2*reach pixels, which read as
// the configured radius never quite being honored. Skip the clamp on
// whichever axis didn't actually find both edges.
float localRadius(float left, float right, float up, float down, float radius, float reach) {
	float capped = reach - 0.6;
	float width = (left < capped && right < capped) ? left + right : 1e6;
	float height = (up < capped && down < capped) ? up + down : 1e6;
	return min(radius, min(width, height) * 0.5);
}

// cornerCoverage supersamples the rounded-corner test over a small grid of
// sub-pixel offsets and averages them into a real coverage value, instead
// of one evaluation blurred by a single smoothstep band. left/up move at
// 1:1 with screen position and right/down at -1:1 (edgeDist's own marching
// units — verified: nudging the query point by (dx, dy) shifts left/up by
// (dx, dy) and right/down by (-dx, -dy)), so a sub-pixel offset can be
// applied to the already-measured distances directly with no extra
// texture sampling. A single sample per pixel is exact for the true
// distance field, but a small-radius circle only spans a handful of
// pixels across its whole quarter-arc, so tracing that field just once per
// pixel still quantizes it into visible stair-steps; averaging several
// jittered samples approximates real multisample coverage the way MSAA
// would, and is what actually reads as round at low radii instead of
// faceted.
const int SS = 4;

float cornerCoverage(float left, float right, float up, float down, float radius, float reach) {
	float sum = 0.0;
	for (int iy = 0; iy < SS; iy++) {
		float dy = (float(iy) + 0.5) / float(SS) - 0.5;
		for (int ix = 0; ix < SS; ix++) {
			float dx = (float(ix) + 0.5) / float(SS) - 0.5;
			float l = left + dx;
			float r = right - dx;
			float u = up + dy;
			float d = down - dy;
			float rr = localRadius(l, r, u, d, radius, reach);
			float k = 1.0;
			k = min(k, cornerKeep(l, u, rr));
			k = min(k, cornerKeep(r, u, rr));
			k = min(k, cornerKeep(r, d, rr));
			k = min(k, cornerKeep(l, d, rr));
			sum += k;
		}
	}
	return sum / float(SS * SS);
}

// Fragment-space corner radius and top-lit gradient over a literal
// non-text surface image. The renderer feeds this pass only
// block/background/border pixels, never text, and this shader only
// samples neighboring pixels from that image — it does not know terminal
// cells or escape sequences. Radius clips convex mask corners by
// measuring, on the four cardinal axes, how close the nearest transparent
// or differently-colored pixel is; gradient reuses that same measurement
// to fake a raised, lit block using only the block's own color, never an
// invented palette. The drop shadow is a separate pass (shadow.frag) that
// reads this pass's rounded output, so its shape exactly matches uRadius
// instead of the sharp rectangle underneath it.
void main() {
	vec4 src = texture(uScene, vUV);

	if (src.a <= 0.001) {
		fragColor = src;
		return;
	}
	if (uRadius <= 0.01 && uGradient <= 0.001) {
		fragColor = src;
		return;
	}

	vec3 base = src.rgb / src.a;
	float radius = min(uRadius, MAX_REACH);
	float reach = max(radius, uGradient > 0.001 ? MAX_REACH : 0.0);
	float left = edgeDist(base, vec2(-1.0, 0.0), reach);
	float right = edgeDist(base, vec2(1.0, 0.0), reach);
	float up = edgeDist(base, vec2(0.0, -1.0), reach);
	float down = edgeDist(base, vec2(0.0, 1.0), reach);

	float keep = 1.0;
	if (radius > 0.01) {
		keep = cornerCoverage(left, right, up, down, radius, reach);
	}

	vec3 rgb = base;
	if (uGradient > 0.001) {
		// t: 0 at the block's top edge, 1 at its bottom edge — lightens
		// toward the top, darkens toward the bottom, like a single soft
		// light from above.
		float t = up / max(up + down, 1e-4);
		float shade = mix(0.3, -0.3, t) * uGradient;
		rgb = max(base * (1.0 + shade), 0.0);
	}

	fragColor = vec4(rgb * src.a * keep, src.a * keep);
}
