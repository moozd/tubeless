#version 330 core

in vec2 vUV;
out vec4 fragColor;

uniform sampler2D uTexture;
uniform float uThreshold;

// Bright-pass extraction: only pixels above uThreshold survive (with a
// soft knee, not a hard cutoff), so the blur chain that follows only
// spreads light from genuinely bright elements — matching how real CRT
// phosphor bloom concentrates on saturated highlights rather than
// uniformly glowing every glyph regardless of brightness.
void main() {
	vec3 c = texture(uTexture, vUV).rgb;
	float lum = dot(c, vec3(0.299, 0.587, 0.114));
	float t = smoothstep(uThreshold - 0.12, uThreshold + 0.12, lum);
	fragColor = vec4(c * t, 1.0);
}
