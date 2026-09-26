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

// srgbToLinear converts one sRGB-encoded channel (0-1) to linear light —
// the inverse of linearToSRGB above, and the mirror of pkg/config's own
// private srgbToLinear (duplicated here rather than exported: this
// package already re-implements that struct's inverse locally for the
// same reason — painting/editing the UI works in raw sRGB bytes, cfg's
// stored values are linear). Used by the custom theme color editor
// (fonts & theme tab) to present/adjust colors as ordinary 0-255 sRGB
// values instead of the linear 0-1 range Config itself stores.
func srgbToLinear(c float32) float32 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return float32(math.Pow(float64((c+0.055)/1.055), 2.4))
}

// srgbByte rounds a linear-light channel to its 0-255 sRGB byte value.
func srgbByte(linear float32) int {
	v := linearToSRGB(linear)
	switch {
	case v <= 0:
		return 0
	case v >= 1:
		return 255
	default:
		return int(v*255 + 0.5)
	}
}

// byteToLinear converts a 0-255 sRGB byte value back to linear light —
// the inverse of srgbByte, for writing an edited channel back into cfg.
func byteToLinear(v int) float32 {
	return srgbToLinear(float32(clampInt(v, 0, 255)) / 255)
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

// xterm256Byte returns one sRGB channel (0=r, 1=g, 2=b) of xterm-256color
// index idx (16-255): the fixed 6x6x6 color cube (16-231) and grayscale
// ramp (232-255) that never change per-theme — only indices 0-15 are ever
// theme-remapped (pkg/config's Colors.Palette). Duplicated here rather
// than exported from pkg/screen's own copy of this same formula, matching
// this codebase's existing precedent of each package keeping its own
// small color-math helpers (pkg/screen/color.go and this file already do
// the same for sRGB<->linear conversions) instead of a cross-package
// dependency for a few lines of arithmetic.
func xterm256Byte(idx, channel int) uint8 {
	if idx >= 232 {
		return uint8(8 + (idx-232)*10)
	}
	idx -= 16
	levels := [6]uint8{0, 95, 135, 175, 215, 255}
	switch channel {
	case 0:
		return levels[(idx/36)%6]
	case 1:
		return levels[(idx/6)%6]
	default:
		return levels[idx%6]
	}
}

// ansi256Bg emits a raw 24-bit SGR background escape for xterm-256color
// index idx (16-255) — a plain reference swatch. Unlike truecolorBg, this
// skips the linear-light round trip: this fixed table has no per-theme
// linear value to store, its bytes are the sRGB value directly.
func ansi256Bg(idx int) string {
	r, g, b := xterm256Byte(idx, 0), xterm256Byte(idx, 1), xterm256Byte(idx, 2)
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
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
