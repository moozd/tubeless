package main

import (
	"fmt"
	"math"
)

// linearToSRGB converts one linear-light channel (0-1) back to sRGB
// (0-1) — the inverse of pkg/config's srgbToLinear. cfg.Colors.Palette
// and cfg.Phosphor.Low/High are stored linear (see pkg/config's doc
// comments), but a terminal truecolor SGR escape expects raw sRGB byte
// values, so painting the UI in the theme's own colors needs this
// conversion first.
func linearToSRGB(c float32) float32 {
	if c <= 0.0031308 {
		return c * 12.92
	}
	return float32(1.055*math.Pow(float64(c), 1/2.4) - 0.055)
}

// toSRGBBytes converts a linear [3]float32 color to clamped 0-255 sRGB
// bytes for an SGR truecolor escape.
func toSRGBBytes(c [3]float32) (r, g, b uint8) {
	conv := func(v float32) uint8 {
		v = linearToSRGB(v)
		switch {
		case v <= 0:
			return 0
		case v >= 1:
			return 255
		default:
			return uint8(v*255 + 0.5)
		}
	}
	return conv(c[0]), conv(c[1]), conv(c[2])
}

// truecolorFg/truecolorBg emit a 24-bit SGR color escape from a linear
// color, for painting real theme colors (accents, palette swatches) into
// the config UI itself instead of the plain 256-color/reverse-video
// palette the rest of the screen still uses.
func truecolorFg(c [3]float32) string {
	r, g, b := toSRGBBytes(c)
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
}

func truecolorBg(c [3]float32) string {
	r, g, b := toSRGBBytes(c)
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
}

// contrastFg returns a near-black or near-white truecolor fg escape,
// whichever reads clearly against bg — used for text drawn directly on a
// pane's solid accent-colored title bar, whose actual color varies by
// theme.
func contrastFg(bg [3]float32) string {
	r, g, b := toSRGBBytes(bg)
	luma := 0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)
	if luma > 140 {
		return "\x1b[38;2;15;15;20m"
	}
	return "\x1b[38;2;235;235;240m"
}

// lerpColor linearly interpolates two linear colors — used for the
// monochrome-theme phosphor ramp swatch (TrueColor themes use their real
// palette instead, see drawSwatches).
func lerpColor(a, b [3]float32, t float32) [3]float32 {
	return [3]float32{
		a[0] + (b[0]-a[0])*t,
		a[1] + (b[1]-a[1])*t,
		a[2] + (b[2]-a[2])*t,
	}
}
