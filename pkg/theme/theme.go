// Package theme holds the phosphor/CRT shader parameters for each theme.
// Values are data, not separate shaders, so the post-process pipeline in
// pkg/render stays a single GLSL program driven by uniforms.
package theme

// Theme is the full set of uniforms the CRT post-process shader needs.
type Theme struct {
	Name string

	// PhosphorLow/High are the dim and bright ends of the single-hue
	// phosphor ramp (linear RGB, 0-1). Cell intensity interpolates between
	// them rather than shifting hue toward white.
	PhosphorLow  [3]float32
	PhosphorHigh [3]float32

	// SoftenAmount blends the whole sharp render toward a blurred copy of
	// itself — this is what keeps *every* edge looking like soft phosphor
	// rather than a crisply rasterized glyph, not just the bright ones.
	SoftenAmount float32
	// BloomStrength adds a separate, more widely blurred, bright-pass-only
	// copy on top — the extra glow that bleeds out from genuinely bright
	// elements (a highlighted button, a waveform peak), on top of the base
	// softening every pixel already gets.
	BloomStrength    float32
	VignetteStrength float32
	CurvatureAmount  float32
}

// Green reproduces the Tektronix TDS 420 CRT, tuned against colors sampled
// directly from the reference photo (~tds-420.JPG): a teal-green phosphor
// (hue ~155-165 deg) that brightens via luminance rather than desaturating
// toward white, and a near-black unlit background.
var Green = Theme{
	Name:             "green",
	PhosphorLow:      [3]float32{0.0, 0.42, 0.30},
	PhosphorHigh:     [3]float32{0.0, 1.0, 0.78},
	SoftenAmount:     0.22,
	BloomStrength:    0.9,
	VignetteStrength: 0.3,
}

// Amber is the canonical DEC amber phosphor look, not tied to a specific
// reference photo.
var Amber = Theme{
	Name:             "amber",
	PhosphorLow:      [3]float32{0.35, 0.16, 0.0},
	PhosphorHigh:     [3]float32{1.0, 0.72, 0.1},
	SoftenAmount:     0.32,
	BloomStrength:    2.2,
	VignetteStrength: 0.25,
}

func ByName(name string) (Theme, bool) {
	switch name {
	case "green":
		return Green, true
	case "amber":
		return Amber, true
	default:
		return Theme{}, false
	}
}
