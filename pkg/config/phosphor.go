package config

// Phosphor primaries for the monochrome themes, derived from real CRT
// phosphor colorimetry instead of hand-picked hex values. green/amber
// presets (config.go) call phosphorColor/dominantWavelengthChromaticity
// below; nothing else in the package depends on this file.

// d65X/d65Y is the CIE 1931 2-degree D65 white point — the reference white
// every sRGB-space calculation (including the render pipeline's own
// GL_FRAMEBUFFER_SRGB) assumes.
const d65X, d65Y = 0.3127, 0.3290

// xyToXYZ converts a CIE 1931 xy chromaticity coordinate plus a target
// luminance Y into CIE XYZ.
func xyToXYZ(x, y, Y float32) [3]float32 {
	if y == 0 {
		return [3]float32{0, 0, 0}
	}
	return [3]float32{x / y * Y, Y, (1 - x - y) / y * Y}
}

// xyzToLinearSRGB is the standard D65 CIE XYZ -> linear-sRGB matrix
// (IEC 61966-2-1). The result is linear light, matching every other color
// value in this package (see srgbToLinear's doc comment) — not yet
// gamut-mapped, since a phosphor's real chromaticity commonly falls
// outside the sRGB triangle (see gamutMapToSRGB).
func xyzToLinearSRGB(xyz [3]float32) [3]float32 {
	x, y, z := xyz[0], xyz[1], xyz[2]
	return [3]float32{
		3.2404542*x - 1.5371385*y - 0.4985314*z,
		-0.9692660*x + 1.8760108*y + 0.0415560*z,
		0.0556434*x - 0.2040259*y + 1.0572252*z,
	}
}

// gamutMapToSRGB brings an out-of-gamut linear-sRGB triple (one or more
// negative channels, from a chromaticity outside the sRGB triangle) back
// into range by desaturating toward white just enough to zero out the most
// negative channel, then renormalizing so the peak channel is exactly 1 —
// a phosphor's brightest achievable render. Desaturating toward white
// preserves hue and lightness direction; a per-channel clamp would shift
// hue by cutting only the offending channel.
func gamutMapToSRGB(rgb [3]float32) [3]float32 {
	var t float32
	for _, c := range rgb {
		if c < 0 {
			if need := -c / (1 - c); need > t {
				t = need
			}
		}
	}
	peak := float32(0)
	for i, c := range rgb {
		rgb[i] = c*(1-t) + t
		if rgb[i] > peak {
			peak = rgb[i]
		}
	}
	if peak > 0 {
		for i := range rgb {
			rgb[i] /= peak
		}
	}
	return rgb
}

// phosphorColor derives a normalized-peak (max channel == 1) linear-RGB
// phosphor primary from a CIE 1931 xy chromaticity coordinate — the
// standard way to turn a phosphor's measured or constructed color point
// into a color a display can actually show.
func phosphorColor(x, y float32) [3]float32 {
	return gamutMapToSRGB(xyzToLinearSRGB(xyToXYZ(x, y, 1)))
}

// locusPoint is one entry of spectralLocus5nm.
type locusPoint struct{ wavelengthNM, x, y float32 }

// spectralLocus5nm is the CIE 1931 2-degree standard observer's spectral
// locus (pure monochromatic light's chromaticity), sampled every 5nm from
// 480-650nm — enough range to cover any visible phosphor hue this package
// needs to construct a chromaticity for. Values are from the CIE's own
// published table (CIE 018:2019, Table 6; files.cie.co.at/Publications-
// datasets/CIE_cc_1931_2deg.csv), not a secondary approximation.
var spectralLocus5nm = []locusPoint{
	{480, 0.09129, 0.13270}, {485, 0.06871, 0.20072}, {490, 0.04539, 0.29498},
	{495, 0.02346, 0.41270}, {500, 0.00817, 0.53842}, {505, 0.00386, 0.65482},
	{510, 0.01387, 0.75019}, {515, 0.03885, 0.81202}, {520, 0.07430, 0.83380},
	{525, 0.11416, 0.82621}, {530, 0.15472, 0.80586}, {535, 0.19288, 0.78163},
	{540, 0.22962, 0.75433}, {545, 0.26578, 0.72432}, {550, 0.30160, 0.69231},
	{555, 0.33736, 0.65885}, {560, 0.37310, 0.62445}, {565, 0.40873, 0.58961},
	{570, 0.44406, 0.55472}, {575, 0.47878, 0.52020}, {580, 0.51249, 0.48659},
	{585, 0.54479, 0.45443}, {590, 0.57515, 0.42423}, {595, 0.60293, 0.39650},
	{600, 0.62704, 0.37249}, {605, 0.64823, 0.35140}, {610, 0.66576, 0.33401},
	{615, 0.68008, 0.31975}, {620, 0.69151, 0.30834}, {625, 0.70061, 0.29930},
	{630, 0.70792, 0.29203}, {635, 0.71403, 0.28593}, {640, 0.71903, 0.28094},
	{645, 0.72303, 0.27695}, {650, 0.72599, 0.27401},
}

// locusXY linearly interpolates spectralLocus5nm at wavelengthNM, clamping
// to the table's ends — the 5nm steps here are close enough together that
// linear interpolation between neighbors tracks the true curve to well
// under 0.001 in x/y, negligible next to a phosphor's own color spread.
func locusXY(wavelengthNM float32) (x, y float32) {
	t := spectralLocus5nm
	if wavelengthNM <= t[0].wavelengthNM {
		return t[0].x, t[0].y
	}
	if n := len(t) - 1; wavelengthNM >= t[n].wavelengthNM {
		return t[n].x, t[n].y
	}
	for i := 1; i < len(t); i++ {
		if wavelengthNM <= t[i].wavelengthNM {
			a, b := t[i-1], t[i]
			f := (wavelengthNM - a.wavelengthNM) / (b.wavelengthNM - a.wavelengthNM)
			return a.x + (b.x-a.x)*f, a.y + (b.y-a.y)*f
		}
	}
	return t[len(t)-1].x, t[len(t)-1].y
}

// dominantWavelengthChromaticity constructs a CIE xy chromaticity from a
// dominant wavelength and purity (0-1, the fraction of the way from the
// D65 white point to that wavelength's spectral-locus point) — the
// standard colorimetric technique for defining a color point when no
// direct measurement exists. Used for amber: unlike green's well-known P1
// phosphor, historical amber CRT phosphors were manufacturer-specific
// blends with no single standardized chromaticity.
func dominantWavelengthChromaticity(wavelengthNM, purity float32) (x, y float32) {
	sx, sy := locusXY(wavelengthNM)
	return d65X + purity*(sx-d65X), d65Y + purity*(sy-d65Y)
}
