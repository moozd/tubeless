#version 330 core

in vec2 vUV;
out vec4 fragColor;

uniform vec2 uPos;        // top-left of the cursor cell, in pixels
uniform vec2 uSize;       // cell size, in pixels
uniform vec2 uScreenSize; // full window size, in pixels
uniform float uBright;    // breathing pulse, 0..1
uniform float uGlow;      // edge anti-aliasing width, in pixels
uniform vec3 uAccent;     // phosphor color for the cursor block

// Renders the terminal cursor as a sharp, slightly rounded phosphor block,
// its intensity modulated by uBright. The whole window is covered so pixels
// far from the cursor output black. It composites into the persistence
// field via soft-add (see persist.frag), so the block glides over the text
// and leaves a short trail as it moves.
void main() {
	// fullscreen.vert does not flip Y (unlike cell.vert): vUV.y = 0 is the
	// bottom of the window. uPos is top-origin (row 0 = top), so invert Y
	// before converting to pixel space.
	vec2 px = vec2(vUV.x, 1.0 - vUV.y) * uScreenSize;
	vec2 halfSize = uSize * 0.5;
	vec2 center = uPos + halfSize;
	float radius = min(halfSize.x, halfSize.y) * 0.35;

	// Signed distance to the rounded rectangle: negative inside, 0 on the
	// boundary, positive outside.
	vec2 q = abs(px - center) - (halfSize - radius);
	float dist = length(max(q, 0.0)) - radius;

	// Soft-edged (anti-aliased) core, no outward-bleeding halo — a crisp
	// block. The breathing envelope (floor included) comes from the CPU as
	// uBright, so here a hidden cursor (uBright == 0) multiplies the shape
	// to exactly nothing — no dim ghost left behind when a program hides
	// the cursor.
	float core = 1.0 - smoothstep(-uGlow, 0.0, dist);
	float v = clamp(core, 0.0, 1.0) * uBright;

	fragColor = vec4(uAccent * v, 1.0);
}
