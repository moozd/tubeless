package render

import (
	"testing"

	"github.com/moozd/tubeless/pkg/config"
	"github.com/moozd/tubeless/pkg/screen"
)

// TestCellColorsCollapsesExplicitBackgroundOnMonochrome pins down the fix
// for a real bug: on a monochrome theme, any cell with an explicit SGR
// background (an LSP hint, a selection highlight, a diff marker — not
// just SGR 7 reverse video) used to compare fg/bg on the raw pre-ramp
// intensity scalar, which can pass Contrast.MinDelta while the two
// colors are still nearly indistinguishable once actually rendered — the
// phosphor ramp's Low endpoint sits well above true black, so a bg-set
// cell (fg and bg both confined to [Low,High]) has far less perceptual
// range available than plain text against real black ever does. A light
// fg color on a "subtle dark" bg — exactly the shape of a typical editor
// hint background — used to render as a near-invisible smear. It must
// now come out as a clean, readable full invert.
func TestCellColorsCollapsesExplicitBackgroundOnMonochrome(t *testing.T) {
	amber := config.Theme("amber")
	cfg := config.Config{
		Phosphor: amber.Phosphor,
		Contrast: config.Contrast{MinDelta: 0.35},
	}

	attr := screen.Attr{
		FgSet: true, Fg: 0.708, // a light editor fg color's luminance
		BgSet: true, Bg: 0.159, // a "subtle dark highlight" bg's luminance
	}
	fg, bg := cellColors(attr, cfg)

	isBlack := fg == [3]float32{0, 0, 0} || bg == [3]float32{0, 0, 0}
	isHigh := fg == cfg.Phosphor.High || bg == cfg.Phosphor.High
	if !isBlack || !isHigh {
		t.Fatalf("cellColors did not collapse to a full invert: fg=%v bg=%v (want one of them pure black and the other Phosphor.High=%v)", fg, bg, cfg.Phosphor.High)
	}
	if fg == bg {
		t.Fatalf("fg == bg == %v — text would be invisible", fg)
	}
}
