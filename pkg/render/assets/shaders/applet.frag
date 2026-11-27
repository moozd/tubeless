#version 330 core

in vec2 vUV;
in vec2 vPixel;
out vec4 fragColor;

uniform sampler2D uImage;
uniform vec4 uPage;   // the full page rect in pixels: x, y, w, h
uniform float uRadius; // surface.radius, in pixels
uniform vec4 uFill;   // a > 0: paint this solid color, not the page

// roundedBoxDist is the signed distance from p (relative to the box
// center) to a box of halfSize with corners rounded by r: negative
// inside, positive outside.
float roundedBoxDist(vec2 p, vec2 halfSize, float r) {
	vec2 q = abs(p) - halfSize + r;
	return length(max(q, 0.0)) + min(max(q.x, q.y), 0.0) - r;
}

// Clips one span of an applet page to the full page's rounded corners.
// A span is only one row of cells, so the corners come from uPage, not
// from the quad being drawn: the top-left corner falls only in the
// first row's span, and a page scrolled half off screen keeps its
// off-screen corners off screen. A page is opaque: its own alpha is
// ignored, so a transparent page background does not show the caster
// block under it. Output is premultiplied, like every other pass that
// blends into the surface and scene layers.
void main() {
	vec2 halfSize = uPage.zw * 0.5;
	float r = min(uRadius, min(halfSize.x, halfSize.y));
	float d = roundedBoxDist(vPixel - uPage.xy - halfSize, halfSize, r);
	float coverage = clamp(0.5 - d, 0.0, 1.0);
	vec3 rgb = uFill.a > 0.0 ? uFill.rgb : texture(uImage, vUV).rgb;
	fragColor = vec4(rgb, 1.0) * coverage;
}
