#version 330 core

in vec2 vUV;
out vec4 fragColor;

uniform sampler2D uScene;     // this frame's rendered scene
uniform sampler2D uPrevAccum; // last frame's decayed accumulator
uniform float uDecay;         // pow(0.05, dt/decaySeconds), computed in Go

// Phosphor persistence: this frame's scene is soft-added over a decayed
// copy of the running accumulator, same screen-blend style as inset.frag's
// cursor soft-add — content that just changed leaves a fading trail
// instead of cutting instantly to black.
void main() {
	vec3 scene = texture(uScene, vUV).rgb;
	vec3 prev = texture(uPrevAccum, vUV).rgb;
	fragColor = vec4(scene + prev * uDecay * (1.0 - scene), 1.0);
}
