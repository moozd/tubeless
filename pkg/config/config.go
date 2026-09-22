// Package config is the single source of truth for every tunable in
// tubeless. It replaces the old pkg/theme: the render pipeline and the
// terminal binary both read a config.Config, and the `config` TUI edits
// one and persists it as TOML under ~/.config/tubeless/config.toml.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Config is the full set of user-settable settings. Nested sections map
// 1:1 to TOML tables so the file reads like the TUI's section list.
//
// Theme and Preset are two independent axes, each named after whichever
// built-in seeded the fields it governs — "custom" once the user edits
// past that seed (see ThemeNames/EffectsPresetNames and the config TUI's
// own doc comment on how editing flips a name to "custom"):
//
//   - Theme governs color: TrueColor, Colors, Phosphor.
//   - Preset governs every other visual effect: Surface, Blur, Cursor,
//     Face, Contrast, CRT. Font/Atlas/Padding/Scrolling/Monochrome sit
//     outside both axes — always user-set directly, never reseeded by
//     either (see the config TUI's "experimental" tab for Scrolling
//     specifically).
type Config struct {
	Theme  string `toml:"theme"`
	Preset string `toml:"preset"`
	// ActiveProfile is the config TUI's own bookkeeping — which saved
	// profile (see SaveProfile/LoadProfile) this Config was last loaded
	// from or saved into, so the TUI can show "profile: X" and keep
	// syncing further edits to it across separate runs, not just within
	// one session. Ignored by the render pipeline entirely; empty means
	// "no profile associated".
	ActiveProfile string     `toml:"active_profile"`
	TrueColor     bool       `toml:"true_color"`
	Font          Font       `toml:"font"`
	Atlas         Atlas      `toml:"atlas"`
	Padding       Padding    `toml:"padding"`
	Phosphor      Phosphor   `toml:"phosphor"`
	Monochrome    Monochrome `toml:"monochrome"`
	Colors        Colors     `toml:"colors"`
	Surface       Surface    `toml:"surface"`
	Blur          Blur       `toml:"blur"`
	Cursor        Cursor     `toml:"cursor"`
	Face          Face       `toml:"face"`
	Contrast      Contrast   `toml:"contrast"`
	Scrollback    Scrollback `toml:"scrollback"`
	Shell         Shell      `toml:"shell"`
	CRT           CRT        `toml:"crt"`
}

// Shell controls how the pty's child process is launched. Lives on the
// config TUI's general settings.
type Shell struct {
	// Program selects which installed shell to launch into. Empty (the
	// default, shown as "auto" in the config TUI) follows $SHELL. "tmux"
	// attaches to (or creates) a fixed session named "home" (see
	// cmd/tubeless's tmuxCommand) instead of a plain login shell, only
	// when tmux is actually on PATH. Any other value names a shell
	// resolved via PATH at launch time (see cmd/tubeless-config's
	// shellChoiceNames for what populates the config TUI's list) —
	// falls back to $SHELL if it's no longer found there. Only takes
	// effect on the auto-detect path; an explicit --shell override
	// always wins.
	Program string `toml:"program"`
}

// Scrollback controls how much scrolled-off history is retained above the
// live screen. Lines <= 0 disables scrollback entirely. Not yet exposed
// in the config TUI — edit the TOML file directly to change it for now.
type Scrollback struct {
	Lines int `toml:"lines"`
}

// Font controls which typeface is rasterized and at what logical size.
// An empty Family selects the bundled FiraCode Nerd Font; otherwise it
// names an installed font family — resolved to an actual file via
// fontconfig (see font.ResolveFamily), not a raw path the user has to
// know themselves. LineHeight scales the font's own line height (1 is
// unchanged); box drawing, block elements, and powerline glyphs are
// generated to fill the resulting cell exactly (see font.Build), so
// they keep tiling seamlessly whatever this is set to.
type Font struct {
	Family     string  `toml:"family"`
	Size       int     `toml:"size"`
	LineHeight float64 `toml:"line_height"`
	// Ligatures enables drawing GSUB programming ligatures (=>, ->, !=, ...)
	// the loaded font itself defines, discovered from its own GSUB table
	// at atlas build time — see font.Faces/Atlas.Ligatures. Off costs
	// nothing beyond a couple of skipped function calls.
	Ligatures bool `toml:"ligatures"`
}

// Atlas controls the offscreen glyph rasterization (see cmd/tubeless's
// atlasScale/fontGamma constants for what these do). Scale <= 0 means
// "auto": cmd/tubeless picks a base scale from the current monitor's own
// content scale instead of using a single flat number for every display
// (see autoAtlasScale) — a standard-DPI panel and a Retina one want
// different base values, not just the same one scaled by dpi.
type Atlas struct {
	Scale int     `toml:"scale"`
	Gamma float64 `toml:"gamma"`
}

// Padding is the empty margin, in framebuffer pixels, kept between the
// window (or letterboxed content box, when an aspect ratio is set) and
// the terminal grid on every side. Sits outside both the Theme and Preset
// axes, like Font/Atlas — always user-set directly, never reseeded.
// Zero (the default) fills the box exactly as before this existed.
type Padding struct {
	Size float32 `toml:"size"`
}

// Phosphor is the two ends of the single-hue phosphor ramp. Cell
// intensity interpolates between Low and High (linear RGB, 0-1) in a
// monochrome theme. A TrueColor theme still uses Low/High for the CRT
// chrome around the real colors — the tube-face tint and the cursor
// accent (see insetpass.go/cursorpass.go) — even though cell fg/bg no
// longer come from this ramp.
type Phosphor struct {
	Low  [3]float32 `toml:"low"`
	High [3]float32 `toml:"high"`
}

// Monochrome controls how a cell's real fg/bg color is chosen along the
// Phosphor ramp — meaningless when TrueColor is on (see cellColors' own
// TrueColor branch, which never consults this at all). Sits outside both
// the Theme and Preset axes, like Font/Atlas/Padding: a rendering
// preference the user sets once, not something a theme or preset switch
// should silently reset.
type Monochrome struct {
	// Mode is "binary" (default, including the zero value ""), "shades",
	// or "spectrum". Under "binary", a cell whose real fg/bg colors land
	// too close together once collapsed onto the ramp (see
	// Contrast.MinDelta) snaps to a hard full-invert: pure black text on
	// pure phosphor-peak, or vice versa — the classic terminal
	// reverse-video block, always legible, but every such cell reads
	// identically regardless of what its actual color was (a git status
	// color, a syntax highlight — this is the "everything looks binary
	// in nvim" complaint termguicolors apps trigger by painting an
	// explicit background on nearly every cell). Under "shades"
	// (experimental), Contrast.MinDelta isn't enforced at all — fg and
	// bg are each translated independently from their own real
	// luminance, the same "trust the app already chose a readable pair"
	// tradeoff TrueColor themes already make (see trueColorCell). An
	// earlier attempt instead pushed a too-close pair apart to still
	// clear MinDelta, but MinDelta (0.35) is larger than any shipped
	// theme's actual ramp span (~0.22 in OKLab lightness), so enforcing
	// it against a bg that's nearly constant across a whole buffer (nvim
	// paints its own background on almost every cell) clipped nearly
	// every foreground to the same Phosphor.High — uniformly overbright,
	// and no longer a stable function of a cell's own color since the
	// result depended on whatever bg it was paired with. "spectrum"
	// (experimental) is "shades"' own quantized brightness ladder
	// (hue-closeness blend off — real hue comes from the cell's own
	// color now, not smuggled into brightness) linearly mixed toward
	// the cell's actual real color by Amount: 0 is the plain phosphor
	// ramp, identical to "shades" with hue_weight pinned to 0; 1 is the
	// cell's real color unmodified, the same passthrough TrueColor
	// itself uses (see trueColorCell). An earlier version instead
	// rotated the rendered hue within a bounded arc around the accent,
	// keeping every color pinned to the ramp's own chroma — replaced
	// because that couldn't express "let the real color all the way
	// through" at all, only a hue-shifted-but-still-phosphor-saturated
	// version of it; a straight mix can sweep the whole range from one
	// endpoint to the other with a single, immediately legible knob.
	Mode string `toml:"mode"`
	// Steps is how many discrete brightness levels "shades" and
	// "spectrum" quantize fg/bg onto, evenly spaced in OKLab lightness
	// across the ramp's own span, instead of a continuous mapping —
	// guarantees every two distinct steps stay at least
	// (ramp span)/(Steps-1) apart in perceived lightness, large enough
	// that any two adjacent steps stay legible stacked as fg-on-bg no
	// matter which two a given app's colors land on. Only consulted
	// when Mode is "shades" or "spectrum"; 0 (the zero value) falls back
	// to a built-in default (see pkg/render's defaultShadeSteps).
	Steps int `toml:"steps"`
	// HueWeight is how much "shades" mode's brightness mapping is
	// pulled toward a cell's closeness to the theme's own accent hue,
	// on top of its real luminance — 0 is luminance only (this mode's
	// original behavior), 1 is hue-closeness only. Only consulted when
	// Mode is "shades" — "spectrum" mixes in the cell's real color
	// directly (see Amount), not through brightness, so it ignores this
	// field entirely. Negative (the zero-adjacent sentinel, since 0
	// is itself a valid explicit choice) falls back to a built-in
	// default (see pkg/render's defaultShadeHueWeight); the config TUI
	// and TOML default to that sentinel via -1.
	HueWeight float32 `toml:"hue_weight"`
	// Amount is how far "spectrum" mode mixes a cell's rendered color
	// from the plain phosphor ramp (0) toward its own real color (1) —
	// see Mode's own doc comment for the two endpoints. Only consulted
	// when Mode is "spectrum". Negative (the zero-adjacent sentinel,
	// since 0 is itself a valid explicit "stay on the ramp" choice)
	// falls back to a built-in default (see pkg/render's
	// defaultSpectrumAmount); the config TUI and TOML default to that
	// sentinel via -1.
	Amount float32 `toml:"amount"`
}

// Colors is a TrueColor theme's color set: DefaultFg/DefaultBg paint a
// cell with no explicit SGR color, and Palette is the theme's own 16-color
// ANSI table (indices 0-7 normal, 8-15 bright) an indexed SGR color
// (30-37/40-47/90-97/100-107, or 38;5;n/48;5;n with n<16 — see
// pkg/screen's Attr.FgIdx/BgIdx) resolves against, instead of a fixed
// xterm table, so `ls --color`/vim/htop's colors match the active theme.
// Both are ignored when TrueColor is false — Phosphor drives every cell in
// the monochrome themes instead. Values are linear RGB (0-1), not raw hex
// — see srgbToLinear.
type Colors struct {
	DefaultFg [3]float32     `toml:"default_fg"`
	DefaultBg [3]float32     `toml:"default_bg"`
	Palette   [16][3]float32 `toml:"palette"`
}

// Surface controls fragment-space treatment of literal block/border pixels.
// It is a post-process filter over a non-text source: escape-sequence output
// is first drawn literally, then this rounded surface is composited under
// images/underlines/text.
type Surface struct {
	Radius   float32 `toml:"radius"`   // corner radius in pixels
	Gradient float32 `toml:"gradient"` // 0..1 top-lit/bottom-shaded sheen, from the block's own color
	Shadow   float32 `toml:"shadow"`   // 0..1 soft layered drop shadow cast down-right, matching the block's rounded corners
}

// Blur controls image-space bloom over the rounded block/border surface.
// It is composited under images/underlines/text, so text stays sharp.
type Blur struct {
	Radius   float32 `toml:"radius"`   // gaussian spread in pixels
	Strength float32 `toml:"strength"` // 0..1 mix of the blurred result
}

// Cursor is the animated cursor's shape, corner rounding, and breathing
// pulse.
type Cursor struct {
	Glow        float32 `toml:"glow"`         // edge anti-aliasing width, in pixels
	PulsePeriod float32 `toml:"pulse_period"` // breathing period in seconds
	// Radius is the at-rest corner rounding, as a fraction (0..1) of the
	// shape's own short half-dimension — 0 is square, 1 is a full
	// stadium/circle. See cursor.frag's uRadius.
	Radius float32 `toml:"radius"`
	// Shape selects the at-rest outline: "block" (default), "bar" (a
	// thin vertical I-beam on the cell's left edge), or "underline" (a
	// thin strip on the cell's bottom edge). The speed-reactive ball/tail
	// morph (see Renderer.UpdateCursor) layers on top of whichever shape
	// is selected.
	Shape string `toml:"shape"`
	// BlinkStyle selects how uBright is driven over time: "ease" (default,
	// a sine breathing pulse), "static" (always fully lit, no pulse), or
	// "hard" (a classic on/off toggle each half of PulsePeriod). Forced to
	// "static" whenever Glass.Enabled is on, regardless of this setting.
	BlinkStyle string `toml:"blink_style"`
	Glass      Glass  `toml:"glass"`
	Trail      Trail  `toml:"trail"`
}

// Trail is the speed-reactive ball/tail morph: the cursor stretches into
// a bullet-shaped head with a tail streaming behind it during a fast
// glide (a jump across the buffer, not ordinary typing), then eases back
// to its at-rest Shape once it settles — see Renderer.UpdateCursor.
// Forced off whenever Glass.Enabled is on, regardless of Enabled here.
//
// Mirrors Neovide's own cursor trail settings (cursor_trail_size,
// cursor_animation_length) rather than exposing this pipeline's own
// speed-threshold internals — Size/Length are the two knobs that
// actually change what you see; the glide speed a jump needs to clear
// before it morphs at all is tuned once (see cursorMorphSpeedLow/High)
// specifically so ordinary typing never triggers it, and isn't something
// a Size/Length change should accidentally undo.
type Trail struct {
	Enabled bool `toml:"enabled"`
	// Size is the tail's reach at full speed, 0 (no tail — just the
	// morphed ball head) .. 1 (the longest tail this pipeline draws).
	Size float32 `toml:"size"`
	// Length is roughly how long, in seconds, the morph itself takes to
	// catch up to its target shape (both growing into the ball+tail on a
	// fast jump, and easing back to the plain shape once it settles) —
	// shorter reads as snappier, longer as a softer, slower transition.
	Length float32 `toml:"length"`
}

// Glass is the experimental macOS-style frosted-glass cursor: instead of
// the normal additive glow, the cursor becomes a translucent panel that
// refracts and blurs the scene behind it, tinted toward the theme's
// accent color. Enabling it forces BlinkStyle to "static" and disables
// the ball/tail speed morph — see Renderer.RenderEffects.
type Glass struct {
	Enabled bool `toml:"enabled"`
	// Tint is how strongly the refracted sample is pulled toward the
	// accent color, 0 (untinted) .. 1 (solid accent).
	Tint float32 `toml:"tint"`
	// Blur is the refraction sample's blur spread, in pixels; 0 disables
	// the blur taps entirely (a single sharp sample).
	Blur float32 `toml:"blur"`
	// Refract is how far the sampled point bends outward from the
	// cursor's center, in pixels; 0 is a plain (unrefracted) sample.
	Refract float32 `toml:"refract"`
	// Opacity is the panel's core opacity, 0 (invisible) .. 1 (opaque).
	Opacity float32 `toml:"opacity"`
}

// Face is the CRT "tube face" look: a faint phosphor-tinted background
// and a radial falloff toward the screen corners. Both are 0..1.
type Face struct {
	BgTint      float32 `toml:"bg_tint"`
	InsetShadow float32 `toml:"inset_shadow"`
}

// Contrast enforces a minimum fg/bg brightness gap on the monochrome
// ramp so apps' isoluminant color pairs stay readable.
type Contrast struct {
	MinDelta float32 `toml:"min_delta"`
}

// srgbToLinear converts one sRGB-encoded channel (0-1, i.e. a raw hex byte
// normalized by /255) to linear light. The render pipeline draws in linear
// light (see pkg/render/window.go's GL_FRAMEBUFFER_SRGB) and re-encodes to
// sRGB on write, so a color fed in already sRGB-encoded — every hex value
// in a published theme's palette — gets brightened a second time, reading
// several times too light with a washed-out, grayish cast. Every preset's
// color constants are run through this once here, at definition time, so
// everything downstream (pkg/render, the shaders) can keep assuming linear
// input. Matches cell_glyph.frag's linearize() GLSL helper.
func srgbToLinear(c float32) float32 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return float32(math.Pow(float64((c+0.055)/1.055), 2.4))
}

// srgb3 builds a linear-light [3]float32 from raw 0-255 sRGB byte values —
// the form every published theme palette's hex colors come in. Every
// preset's color fields are built through this rather than a bare /255, so
// Config's in-memory (and TOML-persisted) representation is always linear.
func srgb3(r, g, b int) [3]float32 {
	return [3]float32{
		srgbToLinear(float32(r) / 255),
		srgbToLinear(float32(g) / 255),
		srgbToLinear(float32(b) / 255),
	}
}

// ThemeColors is the color axis of a look — see Config's own doc comment
// for how this differs from Effects. Colors is meaningless (left zero)
// when TrueColor is false: Phosphor alone drives every cell on a
// monochrome theme.
type ThemeColors struct {
	TrueColor bool
	Phosphor  Phosphor
	Colors    Colors
}

// ThemeNames lists every registered color theme, in the order the config
// TUI's theme cycler steps through them — the single source of truth both
// Theme's switch and the TUI cycle are built from, so they can't drift.
func ThemeNames() []string {
	return []string{
		"rosepine", "rosepine-moon", "gruvbox-dark-hard", "nord", "dracula",
		"catppuccin-mocha", "tokyo-night", "one-dark", "green", "amber",
		"green-p39", "white-p4", "cga", "cyberpunk",
	}
}

// Theme returns the color values a named theme seeds. Unknown or empty
// names fall back to rosepine, the default. green-p39/white-p4/cga are
// period-accurate companions to the monochrome/CGA entries in
// EffectsPresetNames (see monitors.go and MonitorTheme); cyberpunk is
// its own companion to the "cyberpunk" effects preset there.
func Theme(name string) ThemeColors {
	switch name {
	case "amber":
		return amberTheme()
	case "green":
		return greenTheme()
	case "green-p39":
		return greenP39Theme()
	case "white-p4":
		return whiteP4Theme()
	case "cga":
		return cgaTheme()
	case "cyberpunk":
		return cyberpunkTheme()
	case "rosepine-moon":
		return rosepineMoonTheme()
	case "gruvbox-dark-hard":
		return gruvboxDarkHardTheme()
	case "nord":
		return nordTheme()
	case "dracula":
		return draculaTheme()
	case "catppuccin-mocha":
		return catppuccinMochaTheme()
	case "tokyo-night":
		return tokyoNightTheme()
	case "one-dark":
		return oneDarkTheme()
	default:
		return rosepineTheme()
	}
}

// trueColorTheme builds the ThemeColors shape every TrueColor theme
// shares — only the theme's own bg/fg/accent/palette colors differ.
func trueColorTheme(bg, fg, accent [3]float32, palette [16][3]float32) ThemeColors {
	return ThemeColors{TrueColor: true, Phosphor: Phosphor{Low: bg, High: accent}, Colors: Colors{DefaultFg: fg, DefaultBg: bg, Palette: palette}}
}

// DefaultScrollbackLines is how many rows of history every preset
// retains by default — see pkg/screen's Screen.DefaultScrollbackLines,
// which this must match (kept as separate constants since pkg/config
// intentionally doesn't import pkg/screen).
const DefaultScrollbackLines = 5000

// amberTheme uses a phosphor chromaticity constructed from a dominant
// wavelength of 585nm (solidly in the amber/yellow-orange band, 580-600nm)
// at 75% purity, rather than a hand-picked hex value. Unlike green's P1,
// no single standardized "amber CRT phosphor" chromaticity exists —
// historical amber monitors used varying manufacturer-specific blends —
// so this is a defensible construction, not a datasheet lookup: see
// dominantWavelengthChromaticity's doc comment. 75% purity (rather than a
// more fully saturated point closer to the spectral locus) keeps the
// result a warm gold-orange instead of a deeply saturated traffic-orange.
// This is also the theme MonitorTheme links to zenith-zvm-1220's effects
// preset — Zenith's own service manual documents the ZVM-1220 as an
// "amber phosphor CRT" without naming a JEDEC P-number, so it inherits
// this construction rather than a second, redundant one.
func amberTheme() ThemeColors {
	x, y := dominantWavelengthChromaticity(585, 0.75)
	peak := phosphorColor(x, y)
	return ThemeColors{Phosphor: Phosphor{Low: scale3(peak, 0.35), High: peak}}
}

// greenTheme uses the CIE 1931 chromaticity of zinc silicate (Zn2SiO4:Mn,
// "P1"), the classic oscilloscope/radar-display green phosphor — dominant
// wavelength ~525-528nm, xy cross-checked against two independent sources
// at approximately (0.21, 0.71). Real P1 emission is more saturated than
// sRGB's own green primary, so phosphorColor's gamut mapping desaturates
// it back into range; the result reads as a purer, less cyan-shifted green
// than the old hand-picked "spring green" value.
func greenTheme() ThemeColors {
	peak := phosphorColor(0.21, 0.71)
	return ThemeColors{Phosphor: Phosphor{Low: scale3(peak, 0.42), High: peak}}
}

// cyberpunkTheme is a monochrome ramp built around a vivid neon cyan,
// the companion color to the "cyberpunk" effects preset (see
// monitors.go). Unlike amber/green/green-p39/white-p4 above, this isn't
// phosphorColor(x, y) from a real or dominant-wavelength chromaticity —
// this saturated a cyan sits well outside sRGB's own gamut mapping for
// any real phosphor's dominant wavelength, so, same as any TrueColor
// theme's hand-picked accent hex, it's a direct aesthetic pick rather
// than a physically-modeled one.
// Low sits much closer to black than amber/green/etc's ~0.35-0.45 (35-
// 45% of peak): those ratios model real phosphor afterglow, but this is
// a fictional theme free of that constraint, and "shades" mode
// specifically needs the dynamic range — verified against two unrelated
// real syntax-highlighted palettes (rose-pine and catppuccin-mocha, see
// pkg/render's shadeContrastStretch doc comment for the same
// investigation): at Low=0.35*peak, a dozen real tokens' shades only
// spanned sRGB 172-255 (~33% of the 0-255 range) even with hue
// proximity fully weighted in, because that's already most of the way
// to peak — barely any room left below it. At 0.05, the same tokens
// spread over sRGB 83-255 (~67%), each real color landing on a clearly
// distinct shade rather than merely a technically-different one, while
// staying comfortably above the literal-black floor bg-set cells
// render against (see cellColors' own doc comment) so dim text never
// risks blending into the background.
func cyberpunkTheme() ThemeColors {
	peak := srgb3(0x00, 0xf6, 0xff) // vivid neon cyan
	return ThemeColors{Phosphor: Phosphor{Low: scale3(peak, 0.05), High: peak}}
}

// scale3 multiplies every channel of a linear-RGB triple by k — used to
// derive a phosphor ramp's dim/afterglow endpoint (Phosphor.Low) as a
// fraction of its bright peak (Phosphor.High) rather than an independently
// hand-picked color.
func scale3(c [3]float32, k float32) [3]float32 {
	return [3]float32{c[0] * k, c[1] * k, c[2] * k}
}

// rosepineTheme is the default theme: real per-cell color (see
// TrueColor) from the published Rosé Pine palette
// (https://rosepinetheme.com), rather than the monochrome CRT ramp
// green/amber use. Phosphor.Low/High (base / iris) still drive the
// corner vignette tint and the cursor accent, unrelated to per-cell
// color now that TrueColor is on.
func rosepineTheme() ThemeColors {
	return trueColorTheme(
		srgb3(0x19, 0x17, 0x24), // base
		srgb3(0xe0, 0xde, 0xf4), // text
		srgb3(0xc4, 0xa7, 0xe7), // iris
		[16][3]float32{
			srgb3(0x26, 0x23, 0x3a), srgb3(0xeb, 0x6f, 0x92), srgb3(0x31, 0x74, 0x8f), srgb3(0xf6, 0xc1, 0x77),
			srgb3(0x9c, 0xcf, 0xd8), srgb3(0xc4, 0xa7, 0xe7), srgb3(0xeb, 0xbc, 0xba), srgb3(0xe0, 0xde, 0xf4),
			srgb3(0x6e, 0x6a, 0x86), srgb3(0xeb, 0x6f, 0x92), srgb3(0x31, 0x74, 0x8f), srgb3(0xf6, 0xc1, 0x77),
			srgb3(0x9c, 0xcf, 0xd8), srgb3(0xc4, 0xa7, 0xe7), srgb3(0xeb, 0xbc, 0xba), srgb3(0xe0, 0xde, 0xf4),
		})
}

// rosepineMoonTheme is Rosé Pine's "Moon" variant — the same family with
// a slightly different, still-dark base and a couple of accent hues
// swapped (rose instead of foam for cyan's slot).
func rosepineMoonTheme() ThemeColors {
	return trueColorTheme(
		srgb3(0x23, 0x21, 0x36), // base
		srgb3(0xe0, 0xde, 0xf4), // text
		srgb3(0xc4, 0xa7, 0xe7), // iris
		[16][3]float32{
			srgb3(0x39, 0x35, 0x52), srgb3(0xeb, 0x6f, 0x92), srgb3(0x3e, 0x8f, 0xb0), srgb3(0xf6, 0xc1, 0x77),
			srgb3(0x9c, 0xcf, 0xd8), srgb3(0xc4, 0xa7, 0xe7), srgb3(0xea, 0x9a, 0x97), srgb3(0xe0, 0xde, 0xf4),
			srgb3(0x6e, 0x6a, 0x86), srgb3(0xeb, 0x6f, 0x92), srgb3(0x3e, 0x8f, 0xb0), srgb3(0xf6, 0xc1, 0x77),
			srgb3(0x9c, 0xcf, 0xd8), srgb3(0xc4, 0xa7, 0xe7), srgb3(0xea, 0x9a, 0x97), srgb3(0xe0, 0xde, 0xf4),
		})
}

// gruvboxDarkHardTheme is Gruvbox's "hard contrast" dark variant
// (bg0_h) — the darkest of its three background levels.
func gruvboxDarkHardTheme() ThemeColors {
	return trueColorTheme(
		srgb3(0x1d, 0x20, 0x21), // bg0_h
		srgb3(0xeb, 0xdb, 0xb2), // fg1
		srgb3(0xfe, 0x80, 0x19), // orange accent
		[16][3]float32{
			srgb3(0x28, 0x28, 0x28), srgb3(0xcc, 0x24, 0x1d), srgb3(0x98, 0x97, 0x1a), srgb3(0xd7, 0x99, 0x21),
			srgb3(0x45, 0x85, 0x88), srgb3(0xb1, 0x62, 0x86), srgb3(0x68, 0x9d, 0x6a), srgb3(0xa8, 0x99, 0x84),
			srgb3(0x92, 0x83, 0x74), srgb3(0xfb, 0x49, 0x34), srgb3(0xb8, 0xbb, 0x26), srgb3(0xfa, 0xbd, 0x2f),
			srgb3(0x83, 0xa5, 0x98), srgb3(0xd3, 0x86, 0x9b), srgb3(0x8e, 0xc0, 0x7c), srgb3(0xeb, 0xdb, 0xb2),
		})
}

// nordTheme is the Nord palette — polar-night background, snow-storm
// foreground, frost accent.
func nordTheme() ThemeColors {
	return trueColorTheme(
		srgb3(0x2e, 0x34, 0x40), // nord0
		srgb3(0xd8, 0xde, 0xe9), // nord4
		srgb3(0x88, 0xc0, 0xd0), // nord8 (frost)
		[16][3]float32{
			srgb3(0x3b, 0x42, 0x52), srgb3(0xbf, 0x61, 0x6a), srgb3(0xa3, 0xbe, 0x8c), srgb3(0xeb, 0xcb, 0x8b),
			srgb3(0x81, 0xa1, 0xc1), srgb3(0xb4, 0x8e, 0xad), srgb3(0x88, 0xc0, 0xd0), srgb3(0xe5, 0xe9, 0xf0),
			srgb3(0x4c, 0x56, 0x6a), srgb3(0xbf, 0x61, 0x6a), srgb3(0xa3, 0xbe, 0x8c), srgb3(0xeb, 0xcb, 0x8b),
			srgb3(0x81, 0xa1, 0xc1), srgb3(0xb4, 0x8e, 0xad), srgb3(0x8f, 0xbc, 0xbb), srgb3(0xec, 0xef, 0xf4),
		})
}

// draculaTheme is the Dracula palette.
func draculaTheme() ThemeColors {
	return trueColorTheme(
		srgb3(0x28, 0x2a, 0x36), // background
		srgb3(0xf8, 0xf8, 0xf2), // foreground
		srgb3(0xbd, 0x93, 0xf9), // purple accent
		[16][3]float32{
			srgb3(0x21, 0x22, 0x2c), srgb3(0xff, 0x55, 0x55), srgb3(0x50, 0xfa, 0x7b), srgb3(0xf1, 0xfa, 0x8c),
			srgb3(0xbd, 0x93, 0xf9), srgb3(0xff, 0x79, 0xc6), srgb3(0x8b, 0xe9, 0xfd), srgb3(0xf8, 0xf8, 0xf2),
			srgb3(0x62, 0x72, 0xa4), srgb3(0xff, 0x6e, 0x6e), srgb3(0x69, 0xff, 0x94), srgb3(0xff, 0xff, 0xa5),
			srgb3(0xd6, 0xac, 0xff), srgb3(0xff, 0x92, 0xdf), srgb3(0xa4, 0xff, 0xff), srgb3(0xff, 0xff, 0xff),
		})
}

// catppuccinMochaTheme is Catppuccin's darkest flavor, Mocha.
func catppuccinMochaTheme() ThemeColors {
	return trueColorTheme(
		srgb3(0x1e, 0x1e, 0x2e), // base
		srgb3(0xcd, 0xd6, 0xf4), // text
		srgb3(0xcb, 0xa6, 0xf7), // mauve accent
		[16][3]float32{
			srgb3(0x45, 0x47, 0x5a), srgb3(0xf3, 0x8b, 0xa8), srgb3(0xa6, 0xe3, 0xa1), srgb3(0xf9, 0xe2, 0xaf),
			srgb3(0x89, 0xb4, 0xfa), srgb3(0xf5, 0xc2, 0xe7), srgb3(0x94, 0xe2, 0xd5), srgb3(0xba, 0xc2, 0xde),
			srgb3(0x58, 0x5b, 0x70), srgb3(0xf3, 0x8b, 0xa8), srgb3(0xa6, 0xe3, 0xa1), srgb3(0xf9, 0xe2, 0xaf),
			srgb3(0x89, 0xb4, 0xfa), srgb3(0xf5, 0xc2, 0xe7), srgb3(0x94, 0xe2, 0xd5), srgb3(0xa6, 0xad, 0xc8),
		})
}

// tokyoNightTheme is the standard (Night, not Storm/Day) Tokyo Night
// palette.
func tokyoNightTheme() ThemeColors {
	return trueColorTheme(
		srgb3(0x1a, 0x1b, 0x26), // background
		srgb3(0xc0, 0xca, 0xf5), // foreground
		srgb3(0x7a, 0xa2, 0xf7), // blue accent
		[16][3]float32{
			srgb3(0x15, 0x16, 0x1e), srgb3(0xf7, 0x76, 0x8e), srgb3(0x9e, 0xce, 0x6a), srgb3(0xe0, 0xaf, 0x68),
			srgb3(0x7a, 0xa2, 0xf7), srgb3(0xbb, 0x9a, 0xf7), srgb3(0x7d, 0xcf, 0xff), srgb3(0xa9, 0xb1, 0xd6),
			srgb3(0x41, 0x48, 0x68), srgb3(0xf7, 0x76, 0x8e), srgb3(0x9e, 0xce, 0x6a), srgb3(0xe0, 0xaf, 0x68),
			srgb3(0x7a, 0xa2, 0xf7), srgb3(0xbb, 0x9a, 0xf7), srgb3(0x7d, 0xcf, 0xff), srgb3(0xc0, 0xca, 0xf5),
		})
}

// oneDarkTheme is Atom's One Dark palette.
func oneDarkTheme() ThemeColors {
	return trueColorTheme(
		srgb3(0x28, 0x2c, 0x34), // background
		srgb3(0xab, 0xb2, 0xbf), // foreground
		srgb3(0x61, 0xaf, 0xef), // blue accent
		[16][3]float32{
			srgb3(0x28, 0x2c, 0x34), srgb3(0xe0, 0x6c, 0x75), srgb3(0x98, 0xc3, 0x79), srgb3(0xe5, 0xc0, 0x7b),
			srgb3(0x61, 0xaf, 0xef), srgb3(0xc6, 0x78, 0xdd), srgb3(0x56, 0xb6, 0xc2), srgb3(0xab, 0xb2, 0xbf),
			srgb3(0x5c, 0x63, 0x70), srgb3(0xe0, 0x6c, 0x75), srgb3(0x98, 0xc3, 0x79), srgb3(0xe5, 0xc0, 0x7b),
			srgb3(0x61, 0xaf, 0xef), srgb3(0xc6, 0x78, 0xdd), srgb3(0x56, 0xb6, 0xc2), srgb3(0xff, 0xff, 0xff),
		})
}

// DefaultPath is where Load/Save operate unless told otherwise:
// $XDG_CONFIG_HOME/tubeless/config.toml, defaulting to ~/.config —
// deliberately the same on every platform, including macOS, where
// os.UserConfigDir() would otherwise pick ~/Library/Application Support.
func DefaultPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "tubeless", "config.toml"), nil
}

// Load reads path into a Config. The theme used to seed defaults is
// flagTheme when non-empty, otherwise the file's own theme key, otherwise
// rosepine; every field present in the file then overrides the preset. A
// missing file yields the plain preset (never an error).
func Load(path, flagTheme string) (Config, error) {
	cfg := Default()
	if flagTheme != "" {
		applyTheme(&cfg, flagTheme)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}

	var head struct {
		Theme  string `toml:"theme"`
		Preset string `toml:"preset"`
	}
	if err := toml.Unmarshal(data, &head); err != nil {
		return cfg, fmt.Errorf("read theme/preset from %s: %w", path, err)
	}
	if flagTheme == "" && head.Theme != "" {
		applyTheme(&cfg, head.Theme)
	}
	if head.Preset != "" {
		applyEffectsPreset(&cfg, head.Preset)
	}

	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

// Default is the Config a fresh install (or a file-not-found Load) gets:
// the rosepine theme over the modern effects preset — see Config's own
// doc comment on the two axes this seeds.
func Default() Config {
	cfg := Config{
		Font:       Font{Family: "", Size: 14, LineHeight: 1, Ligatures: true},
		Atlas:      Atlas{Scale: 4, Gamma: 1.0},
		Scrollback: Scrollback{Lines: DefaultScrollbackLines},
		// HueWeight/Amount's zero value (0) is each a valid explicit
		// choice ("luminance only" / "stay on the ramp" — see their own
		// doc comments), so neither can double as "unset" the way
		// Mode/Steps' zero values do — seed the -1 sentinel here so a
		// fresh install (or any config.toml that never mentions
		// hue_weight/amount) actually gets the built-in default instead
		// of silently behaving as if it were pinned to 0.
		Monochrome: Monochrome{HueWeight: -1, Amount: -1},
	}
	applyTheme(&cfg, "rosepine")
	applyEffectsPreset(&cfg, "modern")
	return cfg
}

// applyTheme seeds cfg's color axis from a named theme and records the
// name — "custom" is a no-op (its colors are whatever cfg already holds,
// about to be overridden by the TOML unmarshal that follows in Load, or
// by hand in the config TUI).
func applyTheme(cfg *Config, name string) {
	cfg.Theme = name
	if name == "custom" {
		return
	}
	c := Theme(name)
	cfg.TrueColor, cfg.Phosphor, cfg.Colors = c.TrueColor, c.Phosphor, c.Colors
}

// applyEffectsPreset is applyTheme's counterpart for the effects axis —
// see EffectsPreset/Effects.
func applyEffectsPreset(cfg *Config, name string) {
	cfg.Preset = name
	if name == "custom" {
		return
	}
	e := EffectsPreset(name)
	cfg.Surface, cfg.Blur, cfg.Cursor = e.Surface, e.Blur, e.Cursor
	cfg.Face, cfg.Contrast, cfg.CRT = e.Face, e.Contrast, e.CRT
}

// Save writes cfg to path as TOML, creating parent directories as needed.
func Save(path string, cfg Config) error {
	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return permissionHintError(fmt.Errorf("mkdir %s: %w", dir, err), dir)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return permissionHintError(fmt.Errorf("write %s: %w", path, err), dir)
	}
	return nil
}

// permissionHintError appends an ownership hint to err when it's a
// permission failure — the leading suspect for "tubeless config" failing
// to save on a machine where ~/.config/tubeless (or one of its parents)
// was previously created by a `sudo`-run instance and is now root-owned.
// Left as a plain wrap for every other error, so a genuinely different
// cause (a full disk, a read-only filesystem) isn't misattributed.
func permissionHintError(err error, dir string) error {
	if errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("%w (if tubeless was previously run with sudo, this may be root-owned — try: sudo chown -R $(whoami) %s)", err, dir)
	}
	return err
}

// ProfilesDir is where SaveProfile/LoadProfile/ProfileNames operate:
// a "profiles" directory next to config.toml itself.
func ProfilesDir() (string, error) {
	path, err := DefaultPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), "profiles"), nil
}

// profileFilePath resolves name to its file under ProfilesDir,
// sanitizing it first — the config TUI's profile name is free-typed
// user input, and this is the one place it becomes a filesystem path.
func profileFilePath(name string) (string, error) {
	dir, err := ProfilesDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, sanitizeProfileName(name)+".toml"), nil
}

// sanitizeProfileName keeps a profile name to characters safe as a
// single path segment: letters, digits, spaces, dash, underscore — so
// free-typed input can never escape ProfilesDir (no '/', no leading
// '.', nothing that isn't a plain filename).
func sanitizeProfileName(name string) string {
	name = strings.TrimSpace(name)
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == ' ':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return strings.Trim(b.String(), " ._-")
}

// SaveProfile writes cfg as a complete, named snapshot under
// ProfilesDir — a full Config dump (every field this package knows how
// to marshal), the same shape as config.toml itself, so LoadProfile can
// read one straight back with no preset-seeding needed.
func SaveProfile(name string, cfg Config) error {
	path, err := profileFilePath(name)
	if err != nil {
		return err
	}
	if filepath.Base(path) == ".toml" {
		return fmt.Errorf("profile name %q has no usable characters", name)
	}
	return Save(path, cfg)
}

// LoadProfile reads back a snapshot SaveProfile wrote.
func LoadProfile(name string) (Config, error) {
	path, err := profileFilePath(name)
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse profile %s: %w", path, err)
	}
	return cfg, nil
}

// ProfileNames lists every saved profile (base filename, without
// ".toml"), sorted. A missing ProfilesDir (no profile saved yet) yields
// an empty list, never an error.
func ProfileNames() ([]string, error) {
	dir, err := ProfilesDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".toml" {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".toml"))
	}
	sort.Strings(names)
	return names, nil
}
