#version 330 core

in vec2 vUV;
out vec4 fragColor;

uniform sampler2D uScene;

void main() {
	fragColor = texture(uScene, vUV);
}
