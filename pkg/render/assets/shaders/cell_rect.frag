#version 330 core

in vec2 vLocal;
in vec2 vHalfSize;
in vec3 vColor;
in vec4 vRadius; // TL, TR, BR, BL, in pixels

out vec4 fragColor;

// Signed distance to an axis-aligned box with a different corner radius per
// corner (Inigo Quilez's formulation). cellpass.go zeroes a corner's radius
// whenever a same-fill neighbor continues past it, so a run of the same
// background color or block glyph across many cells reads as one
// continuous rounded shape instead of scalloping at every internal seam.
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
