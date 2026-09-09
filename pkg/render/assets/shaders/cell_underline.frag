#version 330 core

in vec2 vLocal;
in vec3 vColor;
in float vStyle;

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
	float baseline = 0.86;
	float thicknessPx = max(1.2, chPx * 0.09);
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
		float wave = baseline + amp * sin(vLocal.x * cycles * 6.2831853);
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
