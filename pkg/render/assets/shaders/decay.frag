#version 330 core

in vec2 vUV;
out vec4 fragColor;

uniform sampler2D uScene;     // this frame's rendered scene
uniform sampler2D uPrevAccum; // last frame's decayed accumulator
uniform float uDecay;         // pow(0.05, dt/decaySeconds), computed in Go

// Phosphor persistence: each pixel shows the brighter of this frame's
// scene and the decayed accumulator, so content that just changed leaves
// a fading trail instead of cutting instantly to black.
//
// It must be max(), not an add: a still image has to settle on its own
// brightness. Adding the decayed copy every frame converges to
// scene / (1 - decay * (1 - scene)), which at 60fps and a 0.18s decay
// lifts a #2d2a2e background to about #4e4a50 and washes the screen gray.
void main() {
	vec3 scene = texture(uScene, vUV).rgb;
	vec3 prev = texture(uPrevAccum, vUV).rgb;
	fragColor = vec4(max(scene, prev * uDecay), 1.0);
}
