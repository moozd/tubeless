#version 330 core

in vec2 vUV;
out vec4 fragColor;

uniform sampler2D uScene;
uniform vec2 uTexel;
uniform float uShadow; // 0..1 dreamy drop shadow strength, cast down-right

// uScene here is surface.frag's already rounded (radius+gradient applied)
// output, not the raw rectangle — so the opaque test below sees the true
// curved silhouette and the shadow it casts follows uRadius exactly
// instead of the sharp corner underneath it.

const float MAX_REACH = 40.0;

// Marches from a transparent point toward dir looking for the nearest
// opaque (real surface) pixel, up to maxDist texels. Step size grows
// geometrically instead of one texel at a time: precision matters near the
// surface, where the shadow is darkest and the falloff curve is steepest,
// and matters far less near maxDist, where the shadow has already faded to
// almost nothing — so a coarser stride out there covers a much longer
// reach for the same sample budget instead of silently truncating it (a
// fixed small iteration cap quietly clips any maxDist bigger than the cap,
// which read as the shadow getting cropped short of where it should fade
// out).
float distToOpaque(vec2 dir, float maxDist) {
	float f = 0.0;
	float step = 1.0;
	for (int i = 0; i < 24; i++) {
		f += step;
		if (f > maxDist) {
			return maxDist;
		}
		if (texture(uScene, vUV + dir * uTexel * f).a > 0.5) {
			return f;
		}
		step *= 1.3;
	}
	return maxDist;
}

// dreamyFalloff sums several soft steps at increasing radii with
// decreasing weight — the "layer many faint, increasingly blurred
// shadows" technique (see Kerry Kolosko's "Drop the Drop Shadows") —
// instead of one hard-edged offset shadow. The weights sum to 1 so, right
// at the casting surface, this still reaches full uShadow strength; moving
// away it fades in a long soft gradient rather than a single sharp band,
// which is what makes a shadow read as ambient depth instead of a smear.
float dreamyFalloff(float d, float reach) {
	float a = 0.0;
	a += 0.34 * (1.0 - smoothstep(0.0, reach * 0.12, d));
	a += 0.26 * (1.0 - smoothstep(0.0, reach * 0.30, d));
	a += 0.20 * (1.0 - smoothstep(0.0, reach * 0.55, d));
	a += 0.13 * (1.0 - smoothstep(0.0, reach * 0.80, d));
	a += 0.07 * (1.0 - smoothstep(0.0, reach, d));
	return clamp(a, 0.0, 1.0);
}

void main() {
	vec4 src = texture(uScene, vUV);
	if (src.a > 0.001 || uShadow <= 0.001) {
		fragColor = src;
		return;
	}

	// Up, left and the up-left diagonal cover the three ways empty
	// background below/right of a block can face it: straight on along
	// either axis, or past its rounded corner. Only these three ever point
	// back toward a down-right-casting block, and a real cast shadow only
	// ever darkens (rgb == 0), never tints — it's an absence of light, not
	// a color from the casting surface.
	float dUp = distToOpaque(vec2(0.0, -1.0), MAX_REACH);
	float dLeft = distToOpaque(vec2(-1.0, 0.0), MAX_REACH);
	float dCorner = distToOpaque(normalize(vec2(-1.0, -1.0)), MAX_REACH);

	float a = max(dreamyFalloff(dUp, MAX_REACH), dreamyFalloff(dLeft, MAX_REACH));
	a = max(a, dreamyFalloff(dCorner, MAX_REACH));
	a *= uShadow;

	fragColor = a <= 0.001 ? vec4(0.0) : vec4(0.0, 0.0, 0.0, a);
}
