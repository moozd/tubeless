#version 330 core

in vec2 vUV;
out vec4 fragColor;

uniform vec2 uPos;        // top-left of the cursor cell, in pixels
uniform vec2 uSize;       // cell size, in pixels
uniform vec2 uScreenSize; // full window size, in pixels
uniform float uBright;    // breathing pulse, 0..1
uniform float uGlow;      // edge anti-aliasing width, in pixels
uniform vec3 uAccent;     // phosphor color for the cursor block
uniform float uMorph;     // 0 = block cursor, 1 = full ball+tail
uniform vec2 uTailDir;    // unit vector, screen space, pointing from the
                          // head's center toward the tail's far tip
uniform float uTailLen;   // tail length, in pixels (0 when not gliding fast)

// Renders the terminal cursor as a sharp, slightly rounded phosphor block
// at rest. As the CPU-side glide speeds up, uMorph rounds the block's
// corners into a full ball and streams a tapering, fading tail out behind
// it along uTailDir; both relax back into the plain block as the glide
// slows, all driven by uMorph so the shape change itself reads as a morph
// rather than a cut. Its intensity is modulated by uBright throughout. The
// whole window is covered so pixels far from the cursor output black. It
// composites into the persistence field via soft-add (see persist.frag),
// so the shape glides over the text and leaves a short trail as it moves.
void main() {
	// fullscreen.vert does not flip Y (unlike cell.vert): vUV.y = 0 is the
	// bottom of the window. uPos is top-origin (row 0 = top), so invert Y
	// before converting to pixel space.
	vec2 px = vec2(vUV.x, 1.0 - vUV.y) * uScreenSize;
	vec2 halfSize = uSize * 0.5;
	vec2 center = uPos + halfSize;

	// The head morphs from the rounded-rect block (full cell aspect, a
	// small corner radius) into a perfect ball inscribed in the cell (a
	// square half-extent equal to its own radius) by interpolating both
	// toward the cell's shorter half-dimension — at uMorph == 1 that makes
	// the rounded-rect SDF below degenerate into an exact circle SDF, so
	// the ball never outgrows the one-cell footprint the block had.
	float minDim = min(halfSize.x, halfSize.y);
	vec2 headHalf = mix(halfSize, vec2(minDim), uMorph);
	float headRadius = mix(minDim * 0.35, minDim, uMorph);

	// Signed distance to the rounded rectangle: negative inside, 0 on the
	// boundary, positive outside.
	vec2 q = abs(px - center) - (headHalf - headRadius);
	float headDist = length(max(q, 0.0)) - headRadius;
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

	fragColor = vec4(uAccent * v, 1.0);
}
