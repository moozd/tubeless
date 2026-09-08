#version 330 core

in vec2 vUV;
out vec4 fragColor;

uniform sampler2D uScene;
uniform sampler2D uSoftenTex;
uniform sampler2D uBloomTex;
uniform float uCurvature;
uniform float uVignette;
uniform float uSoften;
uniform float uBloom;

vec2 barrel(vec2 uv) {
	vec2 c = uv * 2.0 - 1.0;
	float r2 = dot(c, c);
	c *= 1.0 + uCurvature * r2;
	return c * 0.5 + 0.5;
}

void main() {
	vec2 uv = barrel(vUV);
	if (uv.x < 0.0 || uv.x > 1.0 || uv.y < 0.0 || uv.y > 1.0) {
		fragColor = vec4(0.0, 0.0, 0.0, 1.0);
		return;
	}

	vec3 sharp = texture(uScene, uv).rgb;
	vec3 soft = texture(uSoftenTex, uv).rgb;
	vec3 bloom = texture(uBloomTex, uv).rgb;
	vec3 color = mix(sharp, soft, uSoften) + bloom * uBloom;

	vec2 centered = uv - 0.5;
	float vig = 1.0 - uVignette * dot(centered, centered) * 2.0;
	color *= clamp(vig, 0.0, 1.0);

	fragColor = vec4(clamp(color, 0.0, 1.0), 1.0);
}
