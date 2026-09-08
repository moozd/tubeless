package render

import (
	"github.com/moozd/tubeless/pkg/screen"
	"github.com/moozd/tubeless/pkg/theme"
)

// minContrastDelta is the minimum gap enforced between fg and bg
// intensity on the phosphor ramp when both are explicit colors (see
// enforceContrast). Apps routinely pick fg/bg pairs that contrast fine
// by *hue* in a full-color terminal — a cursor-line highlight, a
// selection — but hue doesn't survive a monochrome theme, and two
// isoluminant colors can collapse to nearly the same brightness,
// making the text unreadable against its own background. Ghostty faces
// the same problem for a different reason (accessibility on
// low-contrast color themes) and solves it the same way: force a
// minimum contrast rather than trust the app's color choice blindly.
const minContrastDelta = 0.35

// cellColors resolves a cell's attribute into (foreground, background),
// interpolating the theme's single-hue phosphor ramp by intensity rather
// than introducing separate per-color hues — hue can't survive a
// monochrome theme, but an app's fg/bg color still carries real meaning
// (a git status color, a syntax highlight) as long as its *brightness*
// comes through, which is what attr.Fg/Bg (set from SGR color codes,
// see pkg/screen/sgr.go) give us.
func cellColors(attr screen.Attr, th theme.Theme) (fg, bg [3]float32) {
	fgI := fgIntensity(attr)

	bg = [3]float32{0, 0, 0}
	if !attr.BgSet {
		if attr.Reverse {
			// No explicit background: classic reverse-video (htop's
			// header bar, a status line) — black text on a solid
			// phosphor block.
			return [3]float32{0, 0, 0}, lerp3(th.PhosphorLow, th.PhosphorHigh, fgI)
		}
		if attr.Invisible {
			return bg, bg
		}
		return lerp3(th.PhosphorLow, th.PhosphorHigh, fgI), bg
	}

	bgI := scaleIntensity(attr.Bg)
	bg = lerp3(th.PhosphorLow, th.PhosphorHigh, bgI)
	if attr.Reverse {
		fgI, bgI = bgI, fgI
		bg = lerp3(th.PhosphorLow, th.PhosphorHigh, bgI)
	}
	if attr.Invisible {
		return bg, bg
	}
	return enforceContrast(fgI, bgI, th), bg
}

// enforceContrast pushes fg to the ramp's brightest or dimmest end
// (whichever is farther from bg) when the two intensities are too
// close to read against each other.
func enforceContrast(fgI, bgI float32, th theme.Theme) [3]float32 {
	if abs32(fgI-bgI) >= minContrastDelta {
		return lerp3(th.PhosphorLow, th.PhosphorHigh, fgI)
	}
	if bgI > 0.5 {
		return [3]float32{0, 0, 0}
	}
	return th.PhosphorHigh
}

// fgIntensity picks where on the phosphor ramp a cell's foreground sits.
// An explicit SGR color takes priority over Bold/Dim's default levels,
// but Bold/Dim still nudge it (a bold-and-colored cell should read
// brighter than a plain one of the same color).
func fgIntensity(attr screen.Attr) float32 {
	if !attr.FgSet {
		switch {
		case attr.Bold:
			return 1.0
		case attr.Dim:
			return 0.45
		default:
			return 0.9
		}
	}
	i := scaleIntensity(attr.Fg)
	if attr.Bold {
		i = min32(1.0, i+0.15)
	}
	if attr.Dim {
		i *= 0.6
	}
	return i
}

// scaleIntensity maps a 0-1 luminance onto a floor..1 range on the
// phosphor ramp — a floor above 0 so an app's "black" foreground (e.g.
// terminal.background-on-background spacing tricks aside) still reads as
// dim phosphor rather than vanishing outright; Invisible is the actual
// mechanism for text that should disappear.
func scaleIntensity(luminance float32) float32 {
	const floor = 0.25
	return floor + (1-floor)*luminance
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func lerp3(a, b [3]float32, t float32) [3]float32 {
	return [3]float32{
		a[0] + (b[0]-a[0])*t,
		a[1] + (b[1]-a[1])*t,
		a[2] + (b[2]-a[2])*t,
	}
}
