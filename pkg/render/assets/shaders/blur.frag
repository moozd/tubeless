#version 330 core

in vec2 vUV;
out vec4 fragColor;

uniform sampler2D uTexture;
uniform vec2 uTexelStep; // direction * (1/resolution)

void main() {
	// 9-tap Gaussian, sigma tuned so the kernel visibly softens without
	// washing out entirely in a single pass (bloom.go runs several passes
	// at reduced resolution to build a wide glow cheaply).
	float weights[5] = float[](0.2270270270, 0.1945945946, 0.1216216216, 0.0540540541, 0.0162162162);

	vec3 sum = texture(uTexture, vUV).rgb * weights[0];
	for (int i = 1; i < 5; i++) {
		vec2 offset = uTexelStep * float(i);
		sum += texture(uTexture, vUV + offset).rgb * weights[i];
		sum += texture(uTexture, vUV - offset).rgb * weights[i];
	}
	fragColor = vec4(sum, 1.0);
}
