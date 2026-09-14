#version 330 core

in vec2 vUV;
out vec4 fragColor;

uniform sampler2D uScene;
uniform vec2 uTexel;
uniform float uRadius;

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

float cornerKeep(float a, float b, float radius) {
	if (a >= radius || b >= radius) {
		return 1.0;
	}
	float d = length(vec2(radius - a, radius - b));
	return 1.0 - smoothstep(radius - 0.75, radius + 0.75, d);
}

// Fragment-space corner radius over a literal non-text surface image. The
// renderer feeds this pass only block/background/border pixels, never text,
// and this shader only samples neighboring pixels from that image. It does
// not know terminal cells or escape sequences; it clips convex mask corners
// by measuring, on the four cardinal axes, how close the nearest transparent
// or differently-colored pixel is.
void main() {
	vec4 src = texture(uScene, vUV);
	if (src.a <= 0.001 || uRadius <= 0.01) {
		fragColor = src;
		return;
	}

	vec3 base = src.rgb / src.a;
	float radius = min(uRadius, 16.0);
	float left = edgeDist(base, vec2(-1.0, 0.0), radius);
	float right = edgeDist(base, vec2(1.0, 0.0), radius);
	float up = edgeDist(base, vec2(0.0, -1.0), radius);
	float down = edgeDist(base, vec2(0.0, 1.0), radius);

	float keep = 1.0;
	keep = min(keep, cornerKeep(left, up, radius));
	keep = min(keep, cornerKeep(right, up, radius));
	keep = min(keep, cornerKeep(right, down, radius));
	keep = min(keep, cornerKeep(left, down, radius));

	fragColor = vec4(src.rgb * keep, src.a * keep);
}
