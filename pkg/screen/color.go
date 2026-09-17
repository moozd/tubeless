package screen

import "math"

// The theme's phosphor ramp has no hue to give a color, only brightness —
// these tables and helpers turn an SGR color into the 0-1 luminance an
// app's color choice implies, the same way a real monochrome CRT fed a
// color composite signal would: it never sees hue at all, only the
// signal's own luma (Y') component, computed as the standard Rec.709
// weighted sum of the gamma-encoded R'G'B' — matching cell_glyph.frag's
// luminance() and cmd/tubeless-config's contrastFg, the same formula used
// everywhere else in this codebase a color needs collapsing to one
// number. A saturated blue reading dimmer than a saturated yellow here
// isn't a bug to correct for — that is what actually happens to a color
// signal once you take the hue away, on a real device or here.

var ansi16RGB = [16][3]int{
	{0, 0, 0}, {205, 0, 0}, {0, 205, 0}, {205, 205, 0},
	{0, 0, 238}, {205, 0, 205}, {0, 205, 205}, {229, 229, 229},
	{127, 127, 127}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0},
	{92, 92, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
}

func rgbValue(r, g, b int) float32 {
	return (0.2126*float32(r) + 0.7152*float32(g) + 0.0722*float32(b)) / 255
}

// rgbTriple is rgbValue's truecolor-mode counterpart: the real RGB (0-1)
// instead of the reduced luminance scalar, for pkg/config's TrueColor
// theme (see pkg/render's cellColors). SGR truecolor/256-color params are
// raw sRGB bytes (what every app that emits them, and every published
// palette, means by "red 205"), but the render pipeline draws in linear
// light (see pkg/render/window.go's GL_FRAMEBUFFER_SRGB) — srgbToLinear
// converts once here so every color that reaches a Cell is already
// linear, matching pkg/config's preset colors (see its own srgbToLinear).
func rgbTriple(r, g, b int) [3]float32 {
	return [3]float32{
		srgbToLinear(float32(r) / 255),
		srgbToLinear(float32(g) / 255),
		srgbToLinear(float32(b) / 255),
	}
}

// srgbToLinear converts one sRGB-encoded channel (0-1) to linear light.
// Duplicated from pkg/config's identical helper rather than imported, to
// avoid giving pkg/screen (pure VT-state/parsing) a dependency on
// pkg/config (settings/presets) for one small pure function.
func srgbToLinear(c float32) float32 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return float32(math.Pow(float64((c+0.055)/1.055), 2.4))
}

func ansi16Value(n int) float32 {
	c := ansi16RGB[n&0xf]
	return rgbValue(c[0], c[1], c[2])
}

func ansi16RGBValue(n int) [3]float32 {
	c := ansi16RGB[n&0xf]
	return rgbTriple(c[0], c[1], c[2])
}

// palette256Value covers the standard xterm 256-color palette: 0-15 are
// the ANSI/bright colors above, 16-231 a 6x6x6 RGB cube, 232-255 a
// grayscale ramp.
func palette256Value(n int) float32 {
	switch {
	case n < 0:
		return ansi16Value(0)
	case n < 16:
		return ansi16Value(n)
	case n < 232:
		levels := [6]int{0, 95, 135, 175, 215, 255}
		n -= 16
		return rgbValue(levels[(n/36)%6], levels[(n/6)%6], levels[n%6])
	case n <= 255:
		v := 8 + 10*(n-232)
		return rgbValue(v, v, v)
	default:
		return ansi16Value(7)
	}
}

// palette256RGBValue is palette256Value's truecolor-mode counterpart.
func palette256RGBValue(n int) [3]float32 {
	switch {
	case n < 0:
		return ansi16RGBValue(0)
	case n < 16:
		return ansi16RGBValue(n)
	case n < 232:
		levels := [6]int{0, 95, 135, 175, 215, 255}
		n -= 16
		return rgbTriple(levels[(n/36)%6], levels[(n/6)%6], levels[n%6])
	case n <= 255:
		v := 8 + 10*(n-232)
		return rgbTriple(v, v, v)
	default:
		return ansi16RGBValue(7)
	}
}
