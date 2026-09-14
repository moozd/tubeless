#version 330 core

in vec2 vUV;
out vec4 fragColor;

uniform sampler2D uScene;
uniform vec2 uTexel;
uniform float uRadius;

// isEdge treats a neighbor as a boundary either where the surface goes
// transparent or where it continues with a different flat color — two
// adjacent same-color fills read as one continuous island (no rounding at
// their shared seam), but a color change is a genuine outer edge of the
// fill under the probe, same as the old per-cell neighbor-color check this
// pass replaced.
bool isEdge(vec3 baseColor, vec2 uv) {
	vec4 s = texture(uScene, uv);
	if (s.a <= 0.01) {
		return true;
	}
	return distance(s.rgb, baseColor) > 0.004;
}

float edgeDist(vec3 baseColor, vec2 dir, float radius) {
	for (int i = 1; i <= 16; i++) {
		float f = float(i);
		if (f > radius) {
			break;
		}
		if (isEdge(baseColor, vUV + dir * uTexel * f)) {
			return f - 0.5;
		}
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

	float radius = min(uRadius, 16.0);
	float left = edgeDist(src.rgb, vec2(-1.0, 0.0), radius);
	float right = edgeDist(src.rgb, vec2(1.0, 0.0), radius);
	float up = edgeDist(src.rgb, vec2(0.0, -1.0), radius);
	float down = edgeDist(src.rgb, vec2(0.0, 1.0), radius);

	float keep = 1.0;
	keep = min(keep, cornerKeep(left, up, radius));
	keep = min(keep, cornerKeep(right, up, radius));
	keep = min(keep, cornerKeep(right, down, radius));
	keep = min(keep, cornerKeep(left, down, radius));

	fragColor = vec4(src.rgb * keep, src.a * keep);
}
