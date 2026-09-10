// Package config is the single source of truth for every tunable in
// tubeless. It replaces the old pkg/theme: the render pipeline and the
// terminal binary both read a config.Config, and the `config` TUI edits
// one and persists it as TOML under ~/.config/tubeless/config.toml.
package config

import (
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// Config is the full set of user-settable settings. Nested sections map
// 1:1 to TOML tables so the file reads like the TUI's section list.
type Config struct {
	Theme      string     `toml:"theme"`
	TrueColor  bool       `toml:"true_color"`
	Font       Font       `toml:"font"`
	Atlas      Atlas      `toml:"atlas"`
	Phosphor   Phosphor   `toml:"phosphor"`
	Colors     Colors     `toml:"colors"`
	Blur       Blur       `toml:"blur"`
	Rounding   Rounding   `toml:"rounding"`
	Cursor     Cursor     `toml:"cursor"`
	Face       Face       `toml:"face"`
	Contrast   Contrast   `toml:"contrast"`
	Scrollback Scrollback `toml:"scrollback"`
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
}

// Atlas controls the offscreen glyph rasterization (see cmd/tubeless's
// atlasScale/fontGamma constants for what these do).
type Atlas struct {
	Scale int     `toml:"scale"`
	Gamma float64 `toml:"gamma"`
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

// Blur controls the shape layer's line-art bloom: box-drawing and
// powerline glyphs are soft-added with a gaussian glow that rounds their
// corners and edges. Solid blocks and backgrounds are rounded
// geometrically instead (always on, not configurable here); text stays on
// a separate sharp layer.
type Blur struct {
	Radius   float32 `toml:"radius"`   // gaussian spread in pixels
	Strength float32 `toml:"strength"` // 0..1 mix of the blurred result
}

// Rounding controls the true geometric corner radius applied to
// background fills and single-rect block glyphs (█▀▄▌▐ etc. — see
// cell_rect.frag and font.BlockRect). A corner only rounds where it's a
// genuine outer edge; a run of the same fill across adjacent cells stays
// square at the internal seams. Box-drawing/powerline lines use Blur's
// bloom instead, not this.
type Rounding struct {
	Radius float32 `toml:"radius"` // corner radius, in pixels
}

// Cursor is the animated block cursor's shape and breathing pulse.
type Cursor struct {
	Glow        float32 `toml:"glow"`         // edge anti-aliasing width, in pixels
	PulsePeriod float32 `toml:"pulse_period"` // breathing period in seconds
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

// srgbToLinear3 is srgbToLinear applied channel-wise to an already-0-1
// triple — for the monochrome themes' hand-tuned Phosphor constants, which
// are sRGB-intended values (not built from srgb3's raw byte form) but need
// the same linearization before reaching the linear-light pipeline.
func srgbToLinear3(c [3]float32) [3]float32 {
	return [3]float32{srgbToLinear(c[0]), srgbToLinear(c[1]), srgbToLinear(c[2])}
}

// PresetNames lists every registered theme, in the order the config TUI's
// theme cycler steps through them — the single source of truth both
// Preset's switch and the TUI cycle are built from, so they can't drift.
func PresetNames() []string {
	return []string{
		"rosepine", "rosepine-moon", "gruvbox-dark-hard", "nord", "dracula",
		"catppuccin-mocha", "tokyo-night", "one-dark", "green", "amber",
	}
}

// Preset returns a fully-populated Config seeded from a named theme.
// Unknown or empty names fall back to rosepine, the default. The returned
// value is the baseline that TOML overrides are layered onto by Load.
func Preset(name string) Config {
	switch name {
	case "amber":
		return amberPreset()
	case "green":
		return greenPreset()
	case "rosepine-moon":
		return rosepineMoonPreset()
	case "gruvbox-dark-hard":
		return gruvboxDarkHardPreset()
	case "nord":
		return nordPreset()
	case "dracula":
		return draculaPreset()
	case "catppuccin-mocha":
		return catppuccinMochaPreset()
	case "tokyo-night":
		return tokyoNightPreset()
	case "one-dark":
		return oneDarkPreset()
	default:
		return rosepinePreset()
	}
}

// truecolorPreset builds the shape every TrueColor theme shares — only the
// theme's own bg/fg/accent/palette colors differ between presets; the CRT
// chrome tunables (Blur, Rounding, Cursor, Face, Contrast) are a look
// choice independent of the theme and stay the same baseline across all of
// them, matching the original rosepine preset's hand-tuned values.
func truecolorPreset(name string, bg, fg, accent [3]float32, palette [16][3]float32) Config {
	return Config{
		Theme:      name,
		TrueColor:  true,
		Font:       Font{Family: "", Size: 14, LineHeight: 1},
		Atlas:      Atlas{Scale: 4, Gamma: 1.0},
		Phosphor:   Phosphor{Low: bg, High: accent},
		Colors:     Colors{DefaultFg: fg, DefaultBg: bg, Palette: palette},
		Blur:       Blur{Radius: 2.0, Strength: 0.5},
		Rounding:   Rounding{Radius: 2.0},
		Cursor:     Cursor{Glow: 1.5, PulsePeriod: 0.9},
		Face:       Face{BgTint: 0.04, InsetShadow: 0.3},
		Contrast:   Contrast{MinDelta: 0.35},
		Scrollback: Scrollback{Lines: DefaultScrollbackLines},
	}
}

// DefaultScrollbackLines is how many rows of history every preset
// retains by default — see pkg/screen's Screen.DefaultScrollbackLines,
// which this must match (kept as separate constants since pkg/config
// intentionally doesn't import pkg/screen).
const DefaultScrollbackLines = 5000

func amberPreset() Config {
	return Config{
		Theme: "amber",
		Font:  Font{Family: "", Size: 14, LineHeight: 1},
		Atlas: Atlas{Scale: 4, Gamma: 1.0},
		Phosphor: Phosphor{
			Low:  srgbToLinear3([3]float32{0.35, 0.16, 0.0}),
			High: srgbToLinear3([3]float32{1.0, 0.72, 0.1}),
		},
		Blur:       Blur{Radius: 2.0, Strength: 0.5},
		Rounding:   Rounding{Radius: 2.0},
		Cursor:     Cursor{Glow: 1.5, PulsePeriod: 0.9},
		Face:       Face{BgTint: 0.055, InsetShadow: 0.3},
		Contrast:   Contrast{MinDelta: 0.35},
		Scrollback: Scrollback{Lines: DefaultScrollbackLines},
	}
}

func greenPreset() Config {
	return Config{
		Theme: "green",
		Font:  Font{Family: "", Size: 14, LineHeight: 1},
		Atlas: Atlas{Scale: 4, Gamma: 1.0},
		Phosphor: Phosphor{
			Low:  srgbToLinear3([3]float32{0.0, 0.42, 0.30}),
			High: srgbToLinear3([3]float32{0.0, 1.0, 0.78}),
		},
		Blur:       Blur{Radius: 2.0, Strength: 0.5},
		Rounding:   Rounding{Radius: 2.0},
		Cursor:     Cursor{Glow: 1.5, PulsePeriod: 0.9},
		Face:       Face{BgTint: 0.05, InsetShadow: 0.35},
		Contrast:   Contrast{MinDelta: 0.35},
		Scrollback: Scrollback{Lines: DefaultScrollbackLines},
	}
}

// rosepinePreset is the default theme: real per-cell color (see
// TrueColor) from the published Rosé Pine palette
// (https://rosepinetheme.com), rather than the monochrome CRT ramp
// green/amber use. It keeps the same CRT tube-face chrome as the other
// themes — Phosphor.Low/High (base / iris) still drive the corner
// vignette tint and the cursor accent, unrelated to per-cell color now
// that TrueColor is on.
func rosepinePreset() Config {
	return truecolorPreset("rosepine",
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

// rosepineMoonPreset is Rosé Pine's "Moon" variant — the same family with
// a slightly different, still-dark base and a couple of accent hues
// swapped (rose instead of foam for cyan's slot).
func rosepineMoonPreset() Config {
	return truecolorPreset("rosepine-moon",
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

// gruvboxDarkHardPreset is Gruvbox's "hard contrast" dark variant
// (bg0_h) — the darkest of its three background levels.
func gruvboxDarkHardPreset() Config {
	return truecolorPreset("gruvbox-dark-hard",
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

// nordPreset is the Nord palette — polar-night background, snow-storm
// foreground, frost accent.
func nordPreset() Config {
	return truecolorPreset("nord",
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

// draculaPreset is the Dracula palette.
func draculaPreset() Config {
	return truecolorPreset("dracula",
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

// catppuccinMochaPreset is Catppuccin's darkest flavor, Mocha.
func catppuccinMochaPreset() Config {
	return truecolorPreset("catppuccin-mocha",
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

// tokyoNightPreset is the standard (Night, not Storm/Day) Tokyo Night
// palette.
func tokyoNightPreset() Config {
	return truecolorPreset("tokyo-night",
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

// oneDarkPreset is Atom's One Dark palette.
func oneDarkPreset() Config {
	return truecolorPreset("one-dark",
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
	name := flagTheme
	if name == "" {
		name = "rosepine"
	}
	cfg := Preset(name)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg.Theme = name
			return cfg, nil
		}
		return cfg, err
	}

	if flagTheme == "" {
		var head struct {
			Theme string `toml:"theme"`
		}
		if err := toml.Unmarshal(data, &head); err != nil {
			return cfg, fmt.Errorf("read theme from %s: %w", path, err)
		}
		if head.Theme != "" {
			cfg = Preset(head.Theme)
		}
	}

	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

// Save writes cfg to path as TOML, creating parent directories as needed.
func Save(path string, cfg Config) error {
	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
