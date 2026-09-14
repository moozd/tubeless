#version 330 core

in vec2 vLocal;
in vec2 vHalfSize;
in vec3 vColor;
in vec4 vRadius; // TL, TR, BR, BL, in pixels

out vec4 fragColor;

// Signed distance to an axis-aligned box with a different corner radius per
// corner (Inigo Quilez's formulation). The literal terminal scene currently
// sends zero radii — corners always come out square here — but the SDF
// still antialiases the straight edges via the same smoothstep, which a
// flat hard-fill shortcut would lose; cellpass.go's expandRect overlaps
// same-fill neighbors so that AA fringe doesn't leave a seam between them.
// Any rounded-surface look belongs in a later image-space filter, not in
// cell-neighbor interpretation here.
void main() {
	vec2 p = vLocal;
	vec2 r2 = (p.x > 0.0) ? vRadius.yz : vRadius.xw; // (TR,BR) or (TL,BL)
	float r = (p.y > 0.0) ? r2.y : r2.x;              // bottom or top of that side
	r = min(r, min(vHalfSize.x, vHalfSize.y));

	vec2 q = abs(p) - vHalfSize + r;
	float d = length(max(q, 0.0)) + min(max(q.x, q.y), 0.0) - r;

	float alpha = 1.0 - smoothstep(-0.75, 0.75, d);
	if (alpha <= 0.001) {
		discard;
	}
	// Premultiplied output, paired with a GL_ONE/GL_ONE_MINUS_SRC_ALPHA
	// blend (see cellpass.go) — correct at a rounded corner's AA fringe
	// even where a preceding draw in the same pass (e.g. a background rect
	// under a block glyph rect) left partial alpha there.
	fragColor = vec4(vColor * alpha, alpha);
}
