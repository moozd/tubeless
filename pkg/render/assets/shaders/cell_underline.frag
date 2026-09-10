#version 330 core

in vec2 vLocal;
in vec3 vColor;
in float vStyle;
in vec2 vCellPos;

out vec4 fragColor;

const float STYLE_SINGLE = 1.0;
const float STYLE_DOUBLE = 2.0;
const float STYLE_CURLY  = 3.0;
const float STYLE_DOTTED = 4.0;
const float STYLE_DASHED = 5.0;

uniform vec2 uCellSize;

// Draws the underline decoration named by vStyle (see screen.UnderlineStyle)
// as a thin band near the cell's bottom edge, leaving room below a normal
// glyph's baseline for descenders (g, y, j — drawn after this pass, see
// cellpass.go's DrawUnderline) to still read against it.
void main() {
	float chPx = uCellSize.y;
	float cwPx = uCellSize.x;
	// Underline and undercurl sit a touch closer to the glyph than reads
	// well — nudge just those two styles further down toward the cell's
	// bottom edge. Double/dotted/dashed keep the original baseline.
	float baselineOffset = (vStyle == STYLE_SINGLE || vStyle == STYLE_CURLY) ? 0.03 : 0.0;
	float baseline = 0.86 + baselineOffset;
	float thicknessPx = max(1.2, chPx * 0.09);
	// Plain underline reads too bold at the shared thickness — give it its
	// own, narrower band. Every other style (including undercurl) keeps
	// the shared thickness.
	if (vStyle == STYLE_SINGLE) {
		thicknessPx = max(1.0, chPx * 0.06);
	}
	float thickness = thicknessPx / chPx;
	float halfBand = thickness * 0.6;

	float alpha;
	if (vStyle == STYLE_DOUBLE) {
		float gap = thickness * 1.8;
		float d1 = abs(vLocal.y - (baseline - gap));
		float d2 = abs(vLocal.y - baseline);
		float a1 = 1.0 - smoothstep(halfBand * 0.7, halfBand, d1);
		float a2 = 1.0 - smoothstep(halfBand * 0.7, halfBand, d2);
		alpha = max(a1, a2);
	} else if (vStyle == STYLE_CURLY) {
		float cycles = 1.5; // wave periods per cell width
		float amp = thickness * 1.3;
		float twoPi = 6.2831853;
		// A pure sine reads as mechanically perfect — every hump exactly
		// the same height, drafted rather than drawn. A hand redrawing
		// the same wave never repeats a stroke identically, but each
		// individual stroke is still a single smooth curve — so instead
		// of adding higher-frequency ripple within a hump (jagged, not
		// hand-drawn), this cell's own hump gets a small, stable per-cell
		// jitter to its height and lean, hashed from its grid position so
		// it's the same every frame (no flicker) and different from its
		// neighbors (no two humps alike). The lean is weighted by
		// x*(1-x), which is exactly 0 at both edges, so it only bends the
		// middle of the hump — x=0 and x=1 stay pinned to the baseline
		// exactly like the plain sine did, and adjacent cells' curls
		// still meet without a seam.
		float seed = fract(sin(dot(vCellPos, vec2(12.9898, 78.233))) * 43758.5453);
		float ampJitter = 0.8 + 0.35 * seed;
		float lean = (seed - 0.5) * 1.2;
		float bend = vLocal.x * (1.0 - vLocal.x);
		float wave = baseline + amp * ampJitter * sin(vLocal.x * cycles * twoPi + lean * bend);
		float d = abs(vLocal.y - wave);
		alpha = 1.0 - smoothstep(halfBand * 0.7, halfBand, d);
	} else if (vStyle == STYLE_DOTTED || vStyle == STYLE_DASHED) {
		bool dotted = vStyle == STYLE_DOTTED;
		float periodPx = dotted ? cwPx * 0.22 : cwPx * 0.5;
		float duty = dotted ? 0.5 : 0.6;
		float m = fract((vLocal.x * cwPx) / periodPx);
		float d = abs(vLocal.y - baseline);
		float band = 1.0 - smoothstep(halfBand * 0.7, halfBand, d);
		alpha = band * step(m, duty);
	} else {
		// STYLE_SINGLE and any unrecognized value fall back to a plain line.
		float d = abs(vLocal.y - baseline);
		alpha = 1.0 - smoothstep(halfBand * 0.7, halfBand, d);
	}

	if (alpha <= 0.001) {
		discard;
	}
	// Premultiplied output, paired with a GL_ONE/GL_ONE_MINUS_SRC_ALPHA
	// blend (see cellpass.go's DrawUnderline) — same convention as
	// cell_rect.frag/cell_glyph.frag.
	fragColor = vec4(vColor * alpha, alpha);
}
