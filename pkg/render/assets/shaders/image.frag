#version 330 core

in vec2 vUV;
out vec4 fragColor;

uniform sampler2D uImage;

void main() {
	fragColor = texture(uImage, vUV);
}
