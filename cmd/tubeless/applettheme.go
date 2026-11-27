package main

import (
	"fmt"

	"github.com/moozd/tubeless/pkg/applet"
	"github.com/moozd/tubeless/pkg/config"
)

// appletTheme is the terminal's look for applet pages. A truecolor
// theme gives its own colors; a monochrome (phosphor) theme gives black
// and a 16-step ramp from its dim to its bright phosphor, so an applet
// always gets a full palette in the terminal's hue.
func appletTheme(cfg config.Config, cs *cellSize) applet.Theme {
	fg, bg, palette := themeColors(cfg)
	t := applet.Theme{
		Fg:        hexColor(fg),
		Bg:        hexColor(bg),
		Cursor:    hexColor(fg),
		Selection: applet.Selection{Fg: hexColor(bg), Bg: hexColor(fg)},
		Font:      applet.Font{Family: cfg.Font.Family, Size: float64(cfg.Font.Size)},
		Cell:      applet.Cell{Width: float64(cs.w / cs.dpiX), Height: float64(cs.h / cs.dpiY)},
		IsDark:    luminance(bg) < 0.5,
	}
	for i, c := range palette {
		t.Palette[i] = hexColor(c)
	}
	return t
}

func themeColors(cfg config.Config) (fg, bg [3]float32, palette [16][3]float32) {
	if cfg.TrueColor {
		return cfg.Colors.DefaultFg, cfg.Colors.DefaultBg, cfg.Colors.Palette
	}
	low, high := cfg.Phosphor.Low, cfg.Phosphor.High
	for i := range palette {
		palette[i] = lerp3(low, high, float32(i)/15)
	}
	return high, [3]float32{}, palette
}

func lerp3(a, b [3]float32, t float32) [3]float32 {
	return [3]float32{a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t, a[2] + (b[2]-a[2])*t}
}

// luminance is the relative luminance of a linear-RGB color.
func luminance(c [3]float32) float32 {
	return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
}

func hexColor(c [3]float32) string {
	return fmt.Sprintf("#%02x%02x%02x", config.LinearToSRGB8(c[0]), config.LinearToSRGB8(c[1]), config.LinearToSRGB8(c[2]))
}
