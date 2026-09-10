package render

import "math"

// OKLab conversion (Björn Ottosson, 2020) — used to interpolate the
// monochrome phosphor ramp (see colors.go's lerpPhosphor) in a perceptually
// uniform lightness space instead of linear RGB, where equal steps in the
// blend factor read as visually equal brightness steps.

// linearSRGBToOKLab converts a linear-sRGB triple to OKLab (L, a, b).
func linearSRGBToOKLab(c [3]float32) (L, a, b float32) {
	l := 0.4122214708*c[0] + 0.5363325363*c[1] + 0.0514459929*c[2]
	m := 0.2119034982*c[0] + 0.6806995451*c[1] + 0.1073969566*c[2]
	s := 0.0883024619*c[0] + 0.2817188376*c[1] + 0.6299787005*c[2]
	l, m, s = cbrt32(l), cbrt32(m), cbrt32(s)
	L = 0.2104542553*l + 0.7936177850*m - 0.0040720468*s
	a = 1.9779984951*l - 2.4285922050*m + 0.4505937099*s
	b = 0.0259040371*l + 0.7827717662*m - 0.8086757660*s
	return
}

// oklabToLinearSRGB is linearSRGBToOKLab's inverse.
func oklabToLinearSRGB(L, a, b float32) [3]float32 {
	l := L + 0.3963377774*a + 0.2158037573*b
	m := L - 0.1055613458*a - 0.0638541728*b
	s := L - 0.0894841775*a - 1.2914855480*b
	l, m, s = l*l*l, m*m*m, s*s*s
	return [3]float32{
		4.0767416621*l - 3.3077115913*m + 0.2309699292*s,
		-1.2684380046*l + 2.6097574011*m - 0.3413193965*s,
		-0.0041960863*l - 0.7034186147*m + 1.7076147010*s,
	}
}

// cbrt32 is a real cube root (math.Cbrt, not math.Pow(x, 1.0/3)) — the LMS
// intermediates above can go slightly negative for some inputs, and Pow
// with a fractional exponent produces NaN there instead of the negative
// real root Cbrt correctly returns.
func cbrt32(v float32) float32 {
	return float32(math.Cbrt(float64(v)))
}
