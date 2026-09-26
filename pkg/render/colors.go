package render

import (
	"math"

	"github.com/moozd/tubeless/pkg/config"
	"github.com/moozd/tubeless/pkg/font"
	"github.com/moozd/tubeless/pkg/screen"
)

// cellColors resolves a cell's attribute into (foreground, background). In
// a TrueColor theme (rosepine) it passes the cell's real RGB straight
// through (see trueColorCell); every other theme interpolates the config's
// single-hue phosphor ramp by intensity instead of introducing separate
// per-color hues — hue can't survive a monochrome theme, but an app's
// fg/bg color still carries real meaning (a git status color, a syntax
// highlight) as long as its *brightness* comes through, which is what
// attr.Fg/Bg (set from SGR color codes, see pkg/screen/sgr.go) give us.
func cellColors(attr screen.Attr, cfg config.Config) (fg, bg [3]float32) {
	if cfg.TrueColor {
		return trueColorCell(attr, cfg)
	}
	fgI := fgIntensity(attr)
	fgSrcRGB := sourceRGB(attr.FgIndexed, attr.FgIdx, attr.FgRGB)

	bg = [3]float32{0, 0, 0}
	if !attr.BgSet {
		if attr.Reverse {
			// No explicit background: reverse video is a pure highlight
			// signal (a file-tree selection, a search match, htop's header
			// bar, tmux's own selection/status highlighting). This used to
			// always invert to a flat solid block (black text on
			// Phosphor.High) regardless of the cell's own brightness — a
			// clean invert, but one that makes every reverse-video run look
			// identical, so distinct highlights (e.g. tmux's active vs.
			// inactive status segments) become indistinguishable. Scaling
			// the block by the cell's own intensity instead keeps that
			// distinction, at the accepted cost that a highlighted run
			// whose characters originally carried different colors/icons
			// can now shade unevenly across cells rather than reading as
			// one uniform bar.
			return [3]float32{0, 0, 0}, lerpPhosphor(cfg.Phosphor.Low, cfg.Phosphor.High, fgI)
		}
		if attr.Invisible {
			return bg, bg
		}
		// A real app very often colors text this way — fg-only, no
		// explicit bg — for its *plain* body text specifically (a
		// transparent-background colorscheme, e.g., leaves Normal/
		// Comment/Keyword/String all fg-only, only a handful of
		// highlight groups like Visual/CursorLine ever set bg) — the
		// bulk of a real buffer, not a rare case. "shades"/"spectrum"
		// must still apply here, or they only ever engage on the
		// minority of cells that happen to carry an explicit
		// background, which is exactly the bug this branch used to
		// have: every fg-only cell silently fell back to plain single-
		// hue lerpPhosphor no matter what Mode was set to.
		switch cfg.Monochrome.Mode {
		case "shades":
			fgI = quantizeShade(shadeContrastStretch(shadeIntensity(fgSrcRGB, fgI, cfg.Phosphor.High, shadeHueWeight(cfg))), shadeSteps(cfg))
			return lerpPhosphor(cfg.Phosphor.Low, cfg.Phosphor.High, fgI), bg
		case "spectrum":
			fgI = quantizeShade(shadeContrastStretch(fgI), shadeSteps(cfg))
			mono := lerpPhosphor(cfg.Phosphor.Low, cfg.Phosphor.High, fgI)
			return mixRGB(mono, fgSrcRGB, spectrumAmount(cfg)), bg
		default:
			return lerpPhosphor(cfg.Phosphor.Low, cfg.Phosphor.High, fgI), bg
		}
	}

	// Unlike fgI, bgI is NOT run through scaleIntensity's floor: that
	// floor exists so foreground text can't vanish (see scaleIntensity's
	// own doc comment) — a background fill has nothing to "vanish", it's
	// supposed to be able to go all the way to black when the app's own
	// bg color is genuinely black. Flooring it here anyway used to lift
	// every explicit-bg cell (nearly every cell nvim draws, whose
	// colorscheme paints its own near-black bg on almost everything) to
	// at least ~25% of the way from Phosphor.Low to Phosphor.High,
	// regardless of the theme's Low value — so it never actually looked
	// like a dark terminal background.
	// Background fills ramp from true black, not Phosphor.Low: Low is the
	// dim-but-still-glowing floor text needs so it never fully vanishes
	// (see scaleIntensity), not a meaningful "empty backdrop" color — an
	// app's own near-black bg (nvim's colorscheme, painted on nearly
	// every cell under termguicolors) should read as genuinely dark,
	// matching the ambient black the terminal itself renders outside any
	// bg-set cell (see ambientBG). Flooring bg at Phosphor.Low instead
	// left a visible seam: the empty terminal was true black, but nvim's
	// buffer was always at least Phosphor.Low (a lit, tinted color for a
	// saturated ramp like cyberpunk's) — an overbright-looking buffer
	// background that had nothing to do with the app's own color choice.
	bgSrcRGB := sourceRGB(attr.BgIndexed, attr.BgIdx, attr.BgRGB)
	bgFloor := [3]float32{0, 0, 0}
	bgI := attr.Bg
	bg = lerpPhosphor(bgFloor, cfg.Phosphor.High, bgI)
	if attr.Reverse {
		// The original bg is about to become the rendered TEXT, so it
		// needs fgIntensity's floor after all (reversed text must not
		// vanish either) — apply scaleIntensity here, at the point it
		// takes on that role, rather than unconditionally up front. The
		// original (already-floored) fgI becomes the new fill; it can't
		// go quite as dark as an unfloored value could, a minor
		// imperfection accepted in this one, rarer combination
		// (Reverse together with an explicit bg) to keep the common case
		// above correct without risking reversed text disappearing. Both
		// values now derive from what were text-floor-protected
		// quantities, so the fill also ramps from Phosphor.Low rather
		// than true black here, unlike the un-reversed case below — and
		// the two real colors swap roles right along with the
		// intensities they're paired with, so a "shades" hue comparison
		// below still compares each side against its own real color.
		fgI, bgI = scaleIntensity(attr.Bg), fgI
		fgSrcRGB, bgSrcRGB = bgSrcRGB, fgSrcRGB
		bgFloor = cfg.Phosphor.Low
		bg = lerpPhosphor(bgFloor, cfg.Phosphor.High, bgI)
	}
	if attr.Invisible {
		return bg, bg
	}
	if cfg.Monochrome.Mode == "shades" {
		// Real colors map to discrete shades of the theme's accent —
		// a color should be transformed, not lost. Three steps, each
		// pulling its own weight: shadeIntensity blends luminance with
		// hue-closeness to the accent, not luminance alone (a dozen
		// real syntax colors cluster far too tightly in raw luminance
		// to separate on that signal alone); shadeContrastStretch then
		// spreads that blend away from its own center, since even the
		// hue-aware blend still lands closer together than the 0-1
		// domain suggests — verified this generalizes across unrelated
		// real palettes, not fit to one theme's specific values (see
		// its own doc comment); quantizeShade snaps the result onto a
		// step ladder so any two distinct shades stay at least one
		// step apart in perceived lightness, never collapsing all the
		// way back to binary's 2-level hard invert.
		steps := shadeSteps(cfg)
		hueWeight := shadeHueWeight(cfg)
		fgI = quantizeShade(shadeContrastStretch(shadeIntensity(fgSrcRGB, fgI, cfg.Phosphor.High, hueWeight)), steps)
		bgI = quantizeShade(shadeContrastStretch(shadeIntensity(bgSrcRGB, bgI, cfg.Phosphor.High, hueWeight)), steps)
		return lerpPhosphor(cfg.Phosphor.Low, cfg.Phosphor.High, fgI),
			lerpPhosphor(bgFloor, cfg.Phosphor.High, bgI)
	}
	if cfg.Monochrome.Mode == "spectrum" {
		// Same discrete-lightness-ladder structure as "shades" above,
		// but with no shadeIntensity hue blend — fgI/bgI are plain
		// luminance-derived (shadeContrastStretch + quantizeShade only)
		// — because hue now comes from mixing in the cell's own real
		// color directly (see mixRGB/spectrumAmount), not from folding
		// hue-closeness into brightness. Blending hue into brightness
		// here too would double-count it and reopen exactly the tension
		// defaultShadeHueWeight's own doc comment describes: pulling a
		// hue-close-but-dimmer color's brightness up undoes the
		// luminance hierarchy real syntax highlighting relies on (a
		// comment reading dimmer than body text).
		steps := shadeSteps(cfg)
		amount := spectrumAmount(cfg)
		fgI = quantizeShade(shadeContrastStretch(fgI), steps)
		bgI = quantizeShade(shadeContrastStretch(bgI), steps)
		monoFg := lerpPhosphor(cfg.Phosphor.Low, cfg.Phosphor.High, fgI)
		monoBg := lerpPhosphor(bgFloor, cfg.Phosphor.High, bgI)
		return mixRGB(monoFg, fgSrcRGB, amount), mixRGB(monoBg, bgSrcRGB, amount)
	}
	// Compared in OKLab lightness, not the raw fgI/bgI scalars: a
	// saturated phosphor color's Low endpoint sits well above true black,
	// so its whole [Low,High] span covers far less *perceived* lightness
	// range than a 0-1 scalar gap of the same size implies — a bg-set
	// cell (fg and bg both confined to that span, unlike plain text
	// against real black) can pass a raw-scalar threshold while still
	// rendering two colors the eye can't tell apart. See rampLightness's
	// own doc comment. bgL uses the same bgFloor..High endpoints bg
	// actually rendered with above, so the comparison matches what's
	// drawn.
	fgL := rampLightness(cfg.Phosphor.Low, cfg.Phosphor.High, fgI)
	bgL := rampLightness(bgFloor, cfg.Phosphor.High, bgI)
	if abs32(fgL-bgL) < cfg.Contrast.MinDelta {
		// Too close to read once collapsed onto one hue's brightness —
		// synthesize a full invert (opposite extremes for both fg and
		// bg) instead of only nudging fg, which can leave two still-
		// similar in-between tones rather than a clean, obviously-
		// highlighted block.
		if bgI >= fgI {
			return [3]float32{0, 0, 0}, cfg.Phosphor.High
		}
		return cfg.Phosphor.High, [3]float32{0, 0, 0}
	}
	return lerpPhosphor(cfg.Phosphor.Low, cfg.Phosphor.High, fgI), bg
}

// trueColorCell is cellColors' TrueColor-theme branch: the cell's real RGB
// (attr.FgRGB/BgRGB, set from SGR color codes — see pkg/screen/sgr.go)
// passes straight through instead of collapsing onto a monochrome ramp, so
// contrast isn't auto-enforced here the way the ramp path does — an app
// choosing its own truecolor pair is assumed to have already chosen a
// readable one, same as any other truecolor terminal.
func trueColorCell(attr screen.Attr, cfg config.Config) (fg, bg [3]float32) {
	fg, bg = cfg.Colors.DefaultFg, cfg.Colors.DefaultBg
	if attr.FgSet {
		fg = resolveRGB(attr.FgIndexed, attr.FgIdx, attr.FgRGB, cfg)
	}
	if attr.BgSet {
		bg = resolveRGB(attr.BgIndexed, attr.BgIdx, attr.BgRGB, cfg)
	}
	if attr.Reverse {
		fg, bg = bg, fg
	}
	if attr.Invisible {
		return bg, bg
	}
	if attr.Bold {
		fg = boost3(fg, 0.15)
	}
	if attr.Dim {
		fg = scale3(fg, 0.6)
	}
	return fg, bg
}

// underlineColor is what an underline decoration (see cellpass.go's
// BuildInstances) draws in: the cell's own foreground by default — fg is
// already resolved post-selection-swap by the caller, so the underline
// tracks Reverse/selection the same way the glyph above it does — unless
// SGR 58 set an explicit underline color (Attr.UnderlineColorSet), which
// only has somewhere real to resolve into under a TrueColor theme; the
// monochrome ramp has no separate hue channel for an independent color, so
// it always falls back to fg there.
func underlineColor(attr screen.Attr, fg [3]float32, cfg config.Config) [3]float32 {
	if !cfg.TrueColor || !attr.UnderlineColorSet {
		return fg
	}
	return resolveRGB(attr.UnderlineIndexed, attr.UnderlineIdx, attr.UnderlineRGB, cfg)
}

// resolveRGB returns a cell's actual color: a palette lookup for an
// indexed SGR color (against the *active* theme's Colors.Palette, so a
// live theme switch recolors already-written cells correctly — see
// Attr's doc comment), or the direct RGB already carried on the cell for
// a truecolor/256-cube SGR color.
func resolveRGB(indexed bool, idx int8, direct [3]float32, cfg config.Config) [3]float32 {
	if indexed {
		return cfg.Colors.Palette[idx&0xf]
	}
	return direct
}

// sourceRGB is resolveRGB's monochrome-theme counterpart: a stable,
// theme-independent real color for "shades" mode's hue comparison
// (shadeIntensity), rather than a live-recolorable theme-palette lookup
// — Colors.Palette is meaningless on a monochrome theme anyway (see
// Colors' own doc comment), and this is only ever used as a hue hint,
// never the rendered color itself.
func sourceRGB(indexed bool, idx int8, direct [3]float32) [3]float32 {
	if indexed {
		return screen.IndexedRGB(idx)
	}
	return direct
}

// boost3/scale3 are trueColorCell's RGB analogues of fgIntensity's
// Bold/Dim nudges on the monochrome ramp.
func boost3(c [3]float32, amt float32) [3]float32 {
	return [3]float32{min32(1, c[0]+amt), min32(1, c[1]+amt), min32(1, c[2]+amt)}
}

func scale3(c [3]float32, k float32) [3]float32 {
	return [3]float32{c[0] * k, c[1] * k, c[2] * k}
}

// fgIntensity picks where on the phosphor ramp a cell's foreground sits.
// An explicit SGR color takes priority over Bold/Dim's default levels,
// but Bold/Dim still nudge it (a bold-and-colored cell should read
// brighter than a plain one of the same color). The no-color default
// sits well below Bold rather than right next to it — most of a real
// screen (plain body text, an editor's unstyled majority) has no SGR
// color at all, so pinning it near-maximum brightness read as a flat,
// uniformly harsh wall of light with nothing to set genuine emphasis
// (Bold, an explicit bright color) apart from it.
func fgIntensity(attr screen.Attr) float32 {
	if !attr.FgSet {
		switch {
		case attr.Bold:
			return 1.0
		case attr.Dim:
			return 0.45
		default:
			return 0.65
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

// phosphorRampCache memoizes the OKLab conversion of the last-seen
// Phosphor.Low/High pair for lerpPhosphor — Low/High are constant for an
// entire frame's worth of per-cell calls (every visible cell calls
// lerpPhosphor at least once), so without this a frame would redo the same
// two OKLab conversions thousands of times over.
var (
	phosphorRampLow, phosphorRampHigh       [3]float32
	phosphorRampLowLab, phosphorRampHighLab [3]float32
	phosphorRampCached                      bool
)

// lerpPhosphor blends the phosphor ramp's Low/High endpoints by t (0-1) in
// OKLab lightness space rather than linear RGB — equal steps in t read as
// roughly equal steps in perceived brightness, where a linear-RGB lerp
// visibly compresses the shadow end. The endpoints' own colors (and the
// vividness signal that produces t — see fgIntensity/scaleIntensity above,
// and pkg/screen/color.go's doc comment) are unchanged by this; only the
// blend space is.
func lerpPhosphor(low, high [3]float32, t float32) [3]float32 {
	ensurePhosphorRampCache(low, high)
	L := phosphorRampLowLab[0] + (phosphorRampHighLab[0]-phosphorRampLowLab[0])*t
	a := phosphorRampLowLab[1] + (phosphorRampHighLab[1]-phosphorRampLowLab[1])*t
	b := phosphorRampLowLab[2] + (phosphorRampHighLab[2]-phosphorRampLowLab[2])*t
	return clamp01_3(oklabToLinearSRGB(L, a, b))
}

func ensurePhosphorRampCache(low, high [3]float32) {
	if phosphorRampCached && phosphorRampLow == low && phosphorRampHigh == high {
		return
	}
	phosphorRampLowLab[0], phosphorRampLowLab[1], phosphorRampLowLab[2] = linearSRGBToOKLab(low)
	phosphorRampHighLab[0], phosphorRampHighLab[1], phosphorRampHighLab[2] = linearSRGBToOKLab(high)
	phosphorRampLow, phosphorRampHigh, phosphorRampCached = low, high, true
}

// rampLightness is lerpPhosphor's own L(t), exposed on its own: the
// perceptual-space equivalent of t, since lerpPhosphor blends OKLab
// lightness linearly. cellColors' contrast check compares this instead
// of raw fgI/bgI (see its own doc comment) — t itself only means "0-1
// pre-ramp scalar", not "0-1 of the theme's actual visible lightness
// range", and for a saturated, non-white phosphor color (amber, green)
// that range is much narrower than t's own 0-1 span suggests: Low is
// already fairly light long before t=0, so two t values that look far
// apart numerically can still land almost on top of each other in what
// the eye actually sees.
func rampLightness(low, high [3]float32, t float32) float32 {
	ensurePhosphorRampCache(low, high)
	return phosphorRampLowLab[0] + (phosphorRampHighLab[0]-phosphorRampLowLab[0])*t
}

// mixRGB linearly interpolates two linear-RGB triples by t (0-1) —
// "spectrum" mode's own blend space, deliberately plain linear RGB
// rather than lerpPhosphor's OKLab lightness space: the two endpoints
// here are two independently-meaningful *real* colors (the phosphor
// ramp's own translation, and the cell's actual color), not a single
// ramp's dim-to-bright progression, so there's no shared perceptual
// axis to blend along the way lerpPhosphor's Low/High share one hue.
func mixRGB(a, b [3]float32, t float32) [3]float32 {
	return [3]float32{
		a[0] + (b[0]-a[0])*t,
		a[1] + (b[1]-a[1])*t,
		a[2] + (b[2]-a[2])*t,
	}
}

// defaultSpectrumAmount is "spectrum" mode's built-in mix amount — see
// Monochrome.Amount's own doc comment. Unlike defaultShadeSteps/
// defaultShadeHueWeight below, this hasn't been tuned against a real
// syntax-highlighted buffer yet; it's a first-cut estimate meant to
// still read as "mostly the phosphor ramp, real color showing through"
// rather than "mostly true color", pending the same kind of screenshot
// verification those constants went through.
const defaultSpectrumAmount = 0.4

// shadeSteps resolves Monochrome.Steps' unset sentinel (<=0 — 0 is the
// Go zero value, and a negative step count is meaningless) to
// defaultShadeSteps. Shared by "shades" and "spectrum", both of which
// use it for the same thing: how many rungs their lightness ladder has.
func shadeSteps(cfg config.Config) int {
	if cfg.Monochrome.Steps <= 0 {
		return defaultShadeSteps
	}
	return cfg.Monochrome.Steps
}

// shadeHueWeight resolves Monochrome.HueWeight's unset sentinel
// (negative — see its own doc comment) to defaultShadeHueWeight. Only
// "shades" consults this; "spectrum" mixes in real color directly
// instead (see spectrumAmount).
func shadeHueWeight(cfg config.Config) float32 {
	if cfg.Monochrome.HueWeight < 0 {
		return defaultShadeHueWeight
	}
	return cfg.Monochrome.HueWeight
}

// spectrumAmount resolves Monochrome.Amount's unset sentinel (negative
// — see its own doc comment) to defaultSpectrumAmount, and clamps to
// 0-1 — mixRGB's own contract, so a stray out-of-range config value
// (hand-edited TOML) can't push the mix past either real endpoint.
func spectrumAmount(cfg config.Config) float32 {
	amount := cfg.Monochrome.Amount
	if amount < 0 {
		amount = defaultSpectrumAmount
	}
	return clamp01(amount)
}

// defaultShadeSteps/defaultShadeHueWeight are "shades" mode's built-in
// defaults — see config.Monochrome's Steps/HueWeight doc comments.
//
// Both were tuned against a real syntax-highlighted buffer (rose-pine
// colors) rather than picked by feel. defaultShadeHueWeight started at
// 0.3: baseI (luminance) is already confined to a narrow band by
// scaleIntensity's 0.25 floor plus typical text luminance rarely
// nearing 0 or 1, so at 0.3 the (1-w)*baseI term dominates the blend
// and a dozen different tokens clustered into just 2 of 6 quantized
// buckets — most looked identical. Raising it to 0.8 (paired with
// cyberpunkTheme's Low also being lowered, see its own doc comment) did
// separate most tokens, but let hue proximity override a real luminance
// difference for Comment specifically: rose-pine's Comment is
// genuinely dimmer than Normal (luma 0.56 vs 0.88) but its hue sits
// close enough to a cyan accent that hueWeight=0.8 pulled it back up
// into Normal's own bucket — comments stopped reading as de-emphasized,
// which is the one hierarchy real syntax highlighting always preserves.
// 0.6 keeps that ordering intact (Comment lands a step below Normal)
// while still separating most other tokens by hue. Step count turned
// out to matter just as much as hueWeight for this specific case: at
// steps=6, Comment and Normal collapse back into the same bucket even
// at hueWeight=0.6, and even steps=8 still merged Keyword/Statement
// with Type/Special (all four lean blue-teal, close in hue).
//
// steps=10 was enough for every real rose-pine highlight group to land
// in its own correctly-ordered tier, but MO's actual goal (see
// Monochrome's own doc comment) is stronger than "technically
// distinct": every genuinely different source color should read as a
// clearly different shade at a glance, not just pass a pixel-value
// comparison. shadeContrastStretch (this file) is what actually gets
// there — it spreads the blended value away from its own center
// before quantizing, since real palettes' blended values land far
// closer together than the 0-1 domain suggests. 16 steps gives that
// stretched, now much-wider-spread signal enough resolution to still
// snap cleanly (spot-checked against rose-pine and catppuccin-mocha,
// two unrelated palettes — see shadeContrastStretch's own doc comment).
const (
	defaultShadeSteps     = 16
	defaultShadeHueWeight = 0.6
)

// quantizeShade snaps a 0-1 ramp position to the nearest of steps evenly
// spaced levels. Because lerpPhosphor blends linearly in OKLab lightness
// (see its own doc comment), evenly spaced t means evenly spaced
// *perceived* lightness too — so any two distinct outputs this can
// produce are at least 1/(steps-1) of the ramp's lightness span apart, a
// floor "shades" mode's earlier, unquantized version didn't have.
func quantizeShade(t float32, steps int) float32 {
	if steps <= 1 {
		return t
	}
	n := float32(steps - 1)
	return float32(math.Round(float64(t*n))) / n
}

// shadeIntensity blends a cell's luminance-derived intensity with how
// close its real color's hue sits to the theme's own accent
// (cfg.Phosphor.High) — a color "genuinely in the phosphor's family" (a
// cyan/blue diagnostic under a cyan cyberpunk theme, say) reads more
// prominent than an equally-luminant but hue-distant one (a saturated
// red), instead of the two being indistinguishable once luminance alone
// collapses them. Compared in OKLab's (a,b) chroma plane via cosine
// similarity rather than a hue angle, so it needs no wraparound
// handling. A near-gray source or a desaturated accent has no
// meaningful hue to compare (chroma magnitude near 0) — baseI passes
// through unchanged rather than risk dividing by ~0.
//
// Additive, not multiplicative: multiplying baseI by a proximity factor
// would crush a bright-but-hue-distant color (an LSP error's red, say)
// toward black even at full luminance, hiding real signal instead of
// just reweighting it.
func shadeIntensity(rgb [3]float32, baseI float32, accent [3]float32, hueWeight float32) float32 {
	if hueWeight <= 0 {
		return baseI
	}
	_, sa, sb := linearSRGBToOKLab(rgb)
	_, aa, ab := linearSRGBToOKLab(accent)
	sMag := float32(math.Hypot(float64(sa), float64(sb)))
	aMag := float32(math.Hypot(float64(aa), float64(ab)))
	const minChroma = 0.01
	if sMag < minChroma || aMag < minChroma {
		return baseI
	}
	cos := (sa*aa + sb*ab) / (sMag * aMag)
	proximity := (cos + 1) / 2
	return clamp01((1-hueWeight)*baseI + hueWeight*proximity)
}

// shadeContrastStretchCenter/K tune shadeContrastStretch below.
const (
	shadeContrastStretchCenter = 0.55
	shadeContrastStretchK      = 1.8
)

// shadeContrastStretch spreads a shades-mode blend (shadeIntensity's
// output) away from its own center via a symmetric power curve --
// center stays fixed, values above/below it get pushed toward 1/0
// respectively. Real syntax palettes' blended values land far closer
// together than the 0-1 domain suggests (verified against two
// unrelated real palettes, rose-pine and catppuccin-mocha: a dozen
// distinct source colors each landed within roughly a 0.5-0.9 band
// pre-stretch), so a straight linear quantization wastes most of its
// step ladder on values that never occur. Not a per-theme fit --
// the constants above are applied the same way to any input, chosen
// from the *theoretical* structure of shadeIntensity's blend (baseI's
// own floor, proximity's 0-1 span), not from either test palette's
// specific values.
func shadeContrastStretch(v float32) float32 {
	d := v - shadeContrastStretchCenter
	sign := float32(1)
	if d < 0 {
		sign, d = -1, -d
	}
	maxD := float32(1) - shadeContrastStretchCenter
	if sign < 0 {
		maxD = shadeContrastStretchCenter
	}
	if maxD <= 0 {
		return v
	}
	dn := float32(math.Pow(float64(d/maxD), float64(1/shadeContrastStretchK)))
	return clamp01(shadeContrastStretchCenter + sign*dn*maxD)
}

// clamp01 clamps a scalar to 0-1.
func clamp01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// clamp01_3 clamps each channel of a linear-RGB triple to 0-1 — OKLab's
// round trip can overshoot slightly at the gamut's edge.
func clamp01_3(c [3]float32) [3]float32 {
	return [3]float32{clamp01(c[0]), clamp01(c[1]), clamp01(c[2])}
}

// ambientBG is the "empty terminal" backdrop color BuildInstances skips
// drawing a rect for — plain black for the monochrome themes, or
// cfg.Colors.DefaultFg/Bg's background half for a TrueColor theme (see
// Renderer.RenderScene's DrawAmbientBG, which paints the scene with this
// same color before any rects are drawn). Without this, a TrueColor theme
// would draw literally every default cell as a redundant rect.
func ambientBG(cfg config.Config) [3]float32 {
	if cfg.TrueColor {
		return cfg.Colors.DefaultBg
	}
	return [3]float32{0, 0, 0}
}

// edgeCont bundles which of a filled rect's 4 edges continue into a
// same-color neighbor. cellpass.go overlaps the rect's own geometry
// slightly on those edges — cell_rect.frag antialiases every rect
// independently, and two instances that are merely flush (not overlapping)
// at a shared edge each fade out just short of it, leaving a faint seam
// even though nothing should be visible there.
type edgeCont struct {
	Up, Right, Down, Left bool
}

// sameSurfaceColor is bgRectEdges/blockRectEdges' color-match test: exact
// equality (to float32 precision) rather than a perceptual threshold,
// since a "continuing" edge means the same fill, not a merely similar one.
func sameSurfaceColor(a, b [3]float32) bool {
	const eps = 1.0 / 1024.0
	return abs32(a[0]-b[0]) <= eps && abs32(a[1]-b[1]) <= eps && abs32(a[2]-b[2]) <= eps
}

// bgRectEdges reports which edges of a background fill at (x,y) continue
// into a same-color neighbor cell. grid/cols/rows are the currently-visible
// window (see screen.Screen.VisibleWindow), not necessarily the screen's
// live tail.
func bgRectEdges(grid [][]screen.Cell, cols, rows int, cfg config.Config, x, y int, bg [3]float32) edgeCont {
	same := func(dx, dy int) bool {
		nx, ny := x+dx, y+dy
		if nx < 0 || nx >= cols || ny < 0 || ny >= rows {
			return false
		}
		_, nbg := cellColors(grid[ny][nx].Attr, cfg)
		return sameSurfaceColor(nbg, bg)
	}
	return edgeCont{Up: same(0, -1), Right: same(1, 0), Down: same(0, 1), Left: same(-1, 0)}
}

// blockRectEdges is bgRectEdges' counterpart for a single-rect block glyph
// (font.BlockRect). An edge only continues when the neighbor is also a
// block glyph of the same color whose own sub-rect fully spans this one's
// edge — e.g. a full block beside a left-half block still joins on their
// shared filled edge, but the bottom edge of ▀ (the upper-half block) never
// joins, since nothing can continue across a boundary internal to the cell.
func blockRectEdges(grid [][]screen.Cell, cols, rows int, cfg config.Config, x, y int, fg [3]float32, x0, y0, x1, y1 float32) edgeCont {
	joins := func(dx, dy int) bool {
		nx, ny := x+dx, y+dy
		if nx < 0 || nx >= cols || ny < 0 || ny >= rows {
			return false
		}
		n := grid[ny][nx]
		nx0, ny0, nx1, ny1, ok := font.BlockRect(n.Rune)
		if !ok {
			return false
		}
		nfg, _ := cellColors(n.Attr, cfg)
		if !sameSurfaceColor(nfg, fg) {
			return false
		}
		switch {
		case dx < 0:
			return x0 == 0 && nx1 == 1 && ny0 <= y0 && y1 <= ny1
		case dx > 0:
			return x1 == 1 && nx0 == 0 && ny0 <= y0 && y1 <= ny1
		case dy < 0:
			return y0 == 0 && ny1 == 1 && nx0 <= x0 && x1 <= nx1
		default:
			return y1 == 1 && ny0 == 0 && nx0 <= x0 && x1 <= nx1
		}
	}
	return edgeCont{Up: joins(0, -1), Right: joins(1, 0), Down: joins(0, 1), Left: joins(-1, 0)}
}
