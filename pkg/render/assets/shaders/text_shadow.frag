#version 330 core

in vec2 vUV;
out vec4 fragColor;

// uText is the text layer alone (see Renderer.drawTextShadow): every
// glyph in its own color, premultiplied, over transparent.
uniform sampler2D uText;
uniform vec2 uTexel;
uniform vec2 uOffset;    // full shadow reach, in UV: where the last copy lands
uniform int uDepth;      // copies stacked along uOffset, 1..MAX_DEPTH
uniform float uBlur;     // px softness of each copy's edges
uniform float uTint;     // 0 black .. 1 a darkened shade of the glyph color
uniform float uStrength; // 0..1 final opacity

const int MAX_DEPTH = 32;

// FAR_SHADE is how dark the farthest copy is next to the nearest one:
// the extrusion's sides fade toward the back like a lit 3D object, so
// the layers read as depth instead of one flat smear.
const float FAR_SHADE = 0.55;

// TINT_SHADE darkens the glyph's own color for a tinted shadow — at
// full brightness the shadow would read as a second copy of the text.
const float TINT_SHADE = 0.4;

// sampleText is the text layer at uv, softened by a 3x3 tent filter
// uBlur pixels wide; uBlur 0 is one sharp sample.
vec4 sampleText(vec2 uv) {
	if (uBlur < 0.01) {
		return texture(uText, uv);
	}
	vec2 d = uTexel * uBlur;
	vec4 sum = texture(uText, uv) * 4.0;
	sum += texture(uText, uv + vec2(d.x, 0.0)) * 2.0;
	sum += texture(uText, uv - vec2(d.x, 0.0)) * 2.0;
	sum += texture(uText, uv + vec2(0.0, d.y)) * 2.0;
	sum += texture(uText, uv - vec2(0.0, d.y)) * 2.0;
	sum += texture(uText, uv + d);
	sum += texture(uText, uv - d);
	sum += texture(uText, uv + vec2(d.x, -d.y));
	sum += texture(uText, uv + vec2(-d.x, d.y));
	return sum / 16.0;
}

// Copies are composited front to back: copy 1 (nearest the glyph) sits
// in front, so each farther copy only shows where the nearer ones left
// a gap — the same order a real extruded letter hides its own back.
void main() {
	vec3 color = vec3(0.0);
	float alpha = 0.0;
	for (int i = 1; i <= MAX_DEPTH; i++) {
		if (i > uDepth || alpha > 0.995) {
			break;
		}
		float t = float(i) / float(uDepth);
		vec4 src = sampleText(vUV - uOffset * t);
		if (src.a < 0.001) {
			continue;
		}
		vec3 glyph = src.rgb / src.a;
		float back = float(i - 1) / float(max(uDepth - 1, 1));
		float shade = mix(1.0, FAR_SHADE, back);
		vec3 c = glyph * TINT_SHADE * uTint * shade;
		color += (1.0 - alpha) * c * src.a;
		alpha += (1.0 - alpha) * src.a;
	}
	fragColor = vec4(color, alpha) * uStrength;
}
