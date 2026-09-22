package render

import (
	"testing"

	"github.com/moozd/tubeless/pkg/config"
	"github.com/moozd/tubeless/pkg/screen"
)

// TestCellColorsCollapsesGenuinelyCloseBackgroundOnMonochrome pins down
// the invert path binary mode still needs: on a monochrome theme, a
// cell whose fg/bg land close enough together in *perceived* lightness
// once actually rendered — not just close on the raw pre-ramp intensity
// scalar — must still collapse to a clean, readable full invert rather
// than a near-invisible smear. The close pair is derived from
// rampLightness itself (matching a bg's black..High span against an
// fg's Low..High span) rather than a hand-picked SGR luminance pair, so
// this doesn't depend on exactly how wide either span happens to be for
// a given theme.
func TestCellColorsCollapsesGenuinelyCloseBackgroundOnMonochrome(t *testing.T) {
	amber := config.Theme("amber")
	cfg := config.Config{
		Phosphor: amber.Phosphor,
		Contrast: config.Contrast{MinDelta: 0.35},
	}

	fgI := float32(0.95) // a bright fg's post-scaleIntensity value
	fgL := rampLightness(cfg.Phosphor.Low, cfg.Phosphor.High, fgI)
	highL := rampLightness([3]float32{0, 0, 0}, cfg.Phosphor.High, 1)
	bgI := fgL / highL // a bg intensity landing at the same perceived lightness as fgI

	attr := screen.Attr{
		FgSet: true, Fg: 0.95,
		BgSet: true, Bg: bgI,
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

// TestCellColorsBinaryModeNoLongerOverInverts is a regression guard for
// the bug TestCellColorsCollapsesGenuinelyCloseBackgroundOnMonochrome
// used to pin down instead: comparing bg against Phosphor.Low..High (a
// span narrower than Contrast.MinDelta itself) made almost any bg-set
// cell invert, including genuinely high-contrast pairs like a light fg
// on a real near-black bg. Now that bg ramps from true black, this pair
// has real perceptual range and must render as the plain (non-inverted)
// translation of each side's own color.
func TestCellColorsBinaryModeNoLongerOverInverts(t *testing.T) {
	amber := config.Theme("amber")
	cfg := config.Config{
		Phosphor: amber.Phosphor,
		Contrast: config.Contrast{MinDelta: 0.35},
	}

	attr := screen.Attr{
		FgSet: true, Fg: 0.708, // a light editor fg color's luminance
		BgSet: true, Bg: 0.159, // a genuinely darker bg's luminance
	}
	fg, bg := cellColors(attr, cfg)

	isBlack := fg == [3]float32{0, 0, 0} || bg == [3]float32{0, 0, 0}
	isHigh := fg == cfg.Phosphor.High || bg == cfg.Phosphor.High
	if isBlack && isHigh {
		t.Fatalf("a genuinely high-contrast pair still inverted: fg=%v bg=%v", fg, bg)
	}
	wantFg := lerpPhosphor(cfg.Phosphor.Low, cfg.Phosphor.High, scaleIntensity(attr.Fg))
	wantBg := lerpPhosphor([3]float32{0, 0, 0}, cfg.Phosphor.High, attr.Bg)
	if fg != wantFg || bg != wantBg {
		t.Fatalf("got fg=%v bg=%v, want the plain (non-inverted) translation fg=%v bg=%v", fg, bg, wantFg, wantBg)
	}
}

// TestCellColorsShadesModeQuantizes pins down Monochrome.Mode ==
// "shades": the same too-close fg/bg pair as the binary-mode test above
// must NOT collapse to pure black/Phosphor.High — each side quantizes
// independently onto its own small ladder of accent-color brightness
// steps instead (see quantizeShade), with no pairwise contrast
// correction between fg and bg at all. An earlier version translated fg/
// bg continuously with zero enforcement of any kind; this version keeps
// that "no pairwise coupling" property but guarantees a minimum
// perceptual gap between any two distinct outputs via quantization.
func TestCellColorsShadesModeQuantizes(t *testing.T) {
	amber := config.Theme("amber")
	cfg := config.Config{
		Phosphor:   amber.Phosphor,
		Contrast:   config.Contrast{MinDelta: 0.35},
		Monochrome: config.Monochrome{Mode: "shades"},
	}

	attr := screen.Attr{
		FgSet: true, Fg: 0.708,
		BgSet: true, Bg: 0.159,
	}
	fg, bg := cellColors(attr, cfg)

	if fg == [3]float32{0, 0, 0} || fg == cfg.Phosphor.High {
		t.Fatalf("shades mode still clipped fg to an extreme: fg=%v", fg)
	}
	wantFg := lerpPhosphor(cfg.Phosphor.Low, cfg.Phosphor.High, quantizeShade(shadeContrastStretch(scaleIntensity(attr.Fg)), defaultShadeSteps))
	// bg ramps from true black, not Phosphor.Low — see cellColors' own
	// doc comment on why a background fill's floor differs from text's.
	wantBg := lerpPhosphor([3]float32{0, 0, 0}, cfg.Phosphor.High, quantizeShade(shadeContrastStretch(attr.Bg), defaultShadeSteps))
	if fg != wantFg || bg != wantBg {
		t.Fatalf("shades mode did not quantize fg/bg independently: got fg=%v bg=%v, want fg=%v bg=%v", fg, bg, wantFg, wantBg)
	}
}

// TestCellColorsBackgroundFloorsAtTrueBlack pins down the fix for a real
// bug: an explicit background used to ramp from Phosphor.Low, which for
// a saturated ramp (cyberpunk's, say) is a visibly lit, tinted color —
// not the true black a "genuinely dark app background" (nvim's own
// colorscheme, painted on nearly every cell) needs to match the empty
// terminal around it. A near-black bg must now render at (or very near)
// literal black, not lifted toward Phosphor.Low.
func TestCellColorsBackgroundFloorsAtTrueBlack(t *testing.T) {
	green := config.Theme("green")
	cfg := config.Config{Phosphor: green.Phosphor, Contrast: config.Contrast{MinDelta: 0}}

	attr := screen.Attr{FgSet: true, Fg: 0.9, BgSet: true, Bg: 0}
	_, bg := cellColors(attr, cfg)
	// Compared with a small epsilon, not exact equality: lerpPhosphor
	// round-trips t=0 through an OKLab conversion and back, which can
	// land a few ULPs off {0,0,0}.
	const eps = 1e-4
	for i := range bg {
		if bg[i] > eps || bg[i] < -eps {
			t.Fatalf("a literal-black bg rendered as %v, want ~true black", bg)
		}
	}
}

// TestCellColorsReverseBackgroundStillFloorsAsText guards the one
// deliberate exception to the fix above: when Reverse swaps the original
// bg into the rendered TEXT role, it must still get fgIntensity's floor
// — text is the one thing that must never fully vanish, so a literal
// black original bg reversed into text must not render as literal black
// itself.
func TestCellColorsReverseBackgroundStillFloorsAsText(t *testing.T) {
	green := config.Theme("green")
	cfg := config.Config{Phosphor: green.Phosphor, Contrast: config.Contrast{MinDelta: 0}}

	attr := screen.Attr{FgSet: true, Fg: 0.9, BgSet: true, Bg: 0, Reverse: true}
	fg, _ := cellColors(attr, cfg)
	if fg == cfg.Phosphor.Low {
		t.Fatalf("reversed text sourced from a literal-black bg rendered at the ramp floor (%v) with no legibility floor applied", fg)
	}
}

// TestCellColorsShadesModeConsistentAcrossBackgrounds guards the "too
// local" regression the pairwise-push version had: the same fg color
// must render as the same shade regardless of what bg it happens to be
// paired with — shades mode's whole point is a stable per-color
// translation, not one that depends on neighboring cells.
func TestCellColorsShadesModeConsistentAcrossBackgrounds(t *testing.T) {
	green := config.Theme("green")
	cfg := config.Config{
		Phosphor:   green.Phosphor,
		Contrast:   config.Contrast{MinDelta: 0.35},
		Monochrome: config.Monochrome{Mode: "shades"},
	}

	fg1, _ := cellColors(screen.Attr{FgSet: true, Fg: 0.5, BgSet: true, Bg: 0.05}, cfg)
	fg2, _ := cellColors(screen.Attr{FgSet: true, Fg: 0.5, BgSet: true, Bg: 0.4}, cfg)
	if fg1 != fg2 {
		t.Fatalf("same fg luminance rendered differently depending on bg: %v vs %v", fg1, fg2)
	}
}

// TestQuantizeShade checks quantizeShade snaps onto steps evenly spaced
// levels, and that steps<=1 passes t through unchanged (no division by
// zero).
func TestQuantizeShade(t *testing.T) {
	cases := []struct {
		t     float32
		steps int
		want  float32
	}{
		{0, 6, 0},
		{1, 6, 1},
		{0.5, 6, 0.6},  // round(0.5*5)/5 = round(2.5)/5 = 3/5 (Go rounds .5 away from zero)
		{0.79, 6, 0.8}, // round(0.79*5)/5 = round(3.95)/5 = 4/5
		{0.42, 0, 0.42},
		{0.42, 1, 0.42},
	}
	for _, c := range cases {
		got := quantizeShade(c.t, c.steps)
		if got != c.want {
			t.Errorf("quantizeShade(%v, %d) = %v, want %v", c.t, c.steps, got, c.want)
		}
	}
}

// TestQuantizeShadeStepGapMeetsFloor checks every pair of adjacent steps
// quantizeShade can produce is at least 1/(steps-1) of the ramp's own
// perceived-lightness span apart — the guarantee "shades" mode relies on
// for any two adjacent shades to stay readable stacked as fg-on-bg.
func TestQuantizeShadeStepGapMeetsFloor(t *testing.T) {
	green := config.Theme("green")
	const steps = defaultShadeSteps
	wantGap := (rampLightness([3]float32{0, 0, 0}, green.Phosphor.High, 1) -
		rampLightness([3]float32{0, 0, 0}, green.Phosphor.High, 0)) / float32(steps-1)

	var prevL float32
	for i := 0; i < steps; i++ {
		t32 := quantizeShade(float32(i)/float32(steps-1), steps)
		l := rampLightness([3]float32{0, 0, 0}, green.Phosphor.High, t32)
		if i > 0 {
			gap := l - prevL
			if gap < wantGap-1e-4 {
				t.Errorf("step %d->%d gap %v below the floor %v", i-1, i, gap, wantGap)
			}
		}
		prevL = l
	}
}

// TestShadeIntensityFavorsAccentHue checks a color that's genuinely
// close in hue to the theme's accent scores higher than an equally
// luminant one that isn't — the whole point of blending in hue
// proximity rather than using luminance alone.
func TestShadeIntensityFavorsAccentHue(t *testing.T) {
	cyberpunk := config.Theme("cyberpunk") // neon cyan accent
	accent := cyberpunk.Phosphor.High

	const baseI = 0.5
	const hueWeight = 0.3
	nearAccent := shadeIntensity(accent, baseI, accent, hueWeight)
	farFromAccent := shadeIntensity([3]float32{1, 0, 0}, baseI, accent, hueWeight) // saturated red, ~opposite a cyan accent

	if nearAccent <= farFromAccent {
		t.Fatalf("a color equal to the accent (%v) did not score higher than a hue-distant one (%v)", nearAccent, farFromAccent)
	}
}

// TestShadeIntensityGrayPassesThrough checks a near-gray source color
// (no meaningful hue to compare) leaves baseI unchanged rather than
// risk dividing by a near-zero chroma magnitude.
func TestShadeIntensityGrayPassesThrough(t *testing.T) {
	cyberpunk := config.Theme("cyberpunk")
	const baseI = 0.42
	got := shadeIntensity([3]float32{0.5, 0.5, 0.5}, baseI, cyberpunk.Phosphor.High, 0.3)
	if got != baseI {
		t.Fatalf("gray source changed baseI: got %v, want unchanged %v", got, baseI)
	}
}

// TestShadeIntensityZeroWeightIsLuminanceOnly checks hueWeight<=0 is a
// pure opt-out — the same behavior "shades" mode had before hue
// proximity was added.
func TestShadeIntensityZeroWeightIsLuminanceOnly(t *testing.T) {
	cyberpunk := config.Theme("cyberpunk")
	const baseI = 0.77
	got := shadeIntensity(cyberpunk.Phosphor.High, baseI, cyberpunk.Phosphor.High, 0)
	if got != baseI {
		t.Fatalf("hueWeight=0 changed baseI: got %v, want unchanged %v", got, baseI)
	}
}

// TestShadeContrastStretchFixedPoints checks the center is a fixed
// point and the domain endpoints stay anchored (0 and 1 must still map
// to 0 and 1, or quantizeShade's own darkest/brightest levels would
// drift off the ramp's true Low/High).
func TestShadeContrastStretchFixedPoints(t *testing.T) {
	if got := shadeContrastStretch(shadeContrastStretchCenter); got != float32(shadeContrastStretchCenter) {
		t.Fatalf("center is not a fixed point: got %v, want %v", got, shadeContrastStretchCenter)
	}
	if got := shadeContrastStretch(0); got != 0 {
		t.Fatalf("shadeContrastStretch(0) = %v, want 0", got)
	}
	if got := shadeContrastStretch(1); got != 1 {
		t.Fatalf("shadeContrastStretch(1) = %v, want 1", got)
	}
}

// TestShadeContrastStretchSpreadsAwayFromCenter checks the actual
// "spread" behavior: two values symmetric around the center but close
// to it should end up pushed further apart (toward 0/1) than they
// started, and stay correctly ordered.
func TestShadeContrastStretchSpreadsAwayFromCenter(t *testing.T) {
	below := float32(shadeContrastStretchCenter) - 0.05
	above := float32(shadeContrastStretchCenter) + 0.05
	sBelow := shadeContrastStretch(below)
	sAbove := shadeContrastStretch(above)
	if sBelow >= below {
		t.Fatalf("value below center didn't move further below: got %v, want < %v", sBelow, below)
	}
	if sAbove <= above {
		t.Fatalf("value above center didn't move further above: got %v, want > %v", sAbove, above)
	}
	if !(sBelow < shadeContrastStretchCenter && shadeContrastStretchCenter < sAbove) {
		t.Fatalf("stretch broke ordering around center: below=%v center=%v above=%v", sBelow, shadeContrastStretchCenter, sAbove)
	}
}
