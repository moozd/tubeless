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
	// well — nudge both further down toward the cell's bottom edge.
	// Undercurl needs more clearance than a plain straight line: its own
	// wave amplitude eats into that gap on every upswing, so it gets a
	// bigger push than plain underline to keep clear of descenders.
	// Double/dotted/dashed keep the original baseline.
	float baselineOffset = 0.0;
	if (vStyle == STYLE_SINGLE) {
		baselineOffset = 0.03;
	} else if (vStyle == STYLE_CURLY) {
		baselineOffset = 0.07;
	}
	float baseline = 0.86 + baselineOffset;
	float thicknessPx = max(1.2, chPx * 0.09);
	// Plain underline reads too bold at the shared thickness — give it its
	// own, narrower band. Every other style (including undercurl) keeps
	// the shared thickness.
	if (vStyle == STYLE_SINGLE) {
		thicknessPx = max(1.0, chPx * 0.06);
	}
	// Undercurl reads thicker than a straight line of the same nominal
	// thickness even before any style choice: d below measures vertical
	// distance from the wave, not perpendicular distance, so the band's
	// apparent width stretches wherever the sine is sloped rather than
	// flat at a peak — worst right around its zero-crossings, where the
	// slope is steepest. Needs a noticeably thinner base thickness than
	// even plain underline to end up looking thin once drawn.
	if (vStyle == STYLE_CURLY) {
		thicknessPx = max(1.0, chPx * 0.045);
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
		// Plain sine, same amplitude and phase in every cell — x=0 and
		// x=1 both sit on the baseline, so adjacent cells' curls meet
		// without a seam and the wave reads as one smooth, continuous
		// curve rather than a string of separately-drawn humps. A second
		// harmonic was tried here to fake a hand-drawn wobble, but it
		// pinches the peaks into points instead of keeping them round —
		// reads as jagged, not smooth, so it's gone.
		float wave = baseline + amp * sin(vLocal.x * cycles * twoPi);
		// Signed distance from the band's edge (negative = inside), anti-
		// aliased with fwidth — the screen-space rate of change of that
		// distance — rather than a fixed fraction of halfBand. A fixed
		// fraction only smooths correctly at the specific size it was
		// tuned for: shrink the line's own thickness (as undercurl's is,
		// deliberately thin) and that same fraction shrinks below a
		// pixel, so it stops actually blending anything and reads as
		// jagged/pixelated instead of smooth. fwidth grows the AA band
		// wherever the curve moves fast per pixel — its steep
		// zero-crossings especially — so it stays smooth there too,
		// without needing to be re-tuned by hand for this thickness.
		float d = abs(vLocal.y - wave) - halfBand;
		float aa = max(fwidth(d), 0.0005);
		alpha = 1.0 - smoothstep(-aa, aa, d);
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
