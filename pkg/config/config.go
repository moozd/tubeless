// Package config is the single source of truth for every tunable in
// tubeless. It replaces the old pkg/theme: the render pipeline and the
// terminal binary both read a config.Config, and the `config` TUI edits
// one and persists it as TOML under ~/.config/tubeless/config.toml.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// Config is the full set of user-settable settings. Nested sections map
// 1:1 to TOML tables so the file reads like the TUI's section list.
type Config struct {
	Theme     string   `toml:"theme"`
	TrueColor bool     `toml:"true_color"`
	Font      Font     `toml:"font"`
	Atlas     Atlas    `toml:"atlas"`
	Phosphor  Phosphor `toml:"phosphor"`
	Colors    Colors   `toml:"colors"`
	Blur      Blur     `toml:"blur"`
	Rounding  Rounding `toml:"rounding"`
	Cursor    Cursor   `toml:"cursor"`
	Face      Face     `toml:"face"`
	Contrast  Contrast `toml:"contrast"`
}

// Font controls which typeface is rasterized and at what logical size.
// An empty Family selects the bundled FiraCode Nerd Font; otherwise it
// names an installed font family — resolved to an actual file via
// fontconfig (see font.ResolveFamily), not a raw path the user has to
// know themselves.
type Font struct {
	Family string `toml:"family"`
	Size   int    `toml:"size"`
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

// Colors is the default fg/bg for a TrueColor theme: the color painted
// for a cell with no explicit SGR color. Ignored when TrueColor is false
// — Phosphor drives every cell in the monochrome themes instead.
type Colors struct {
	DefaultFg [3]float32 `toml:"default_fg"`
	DefaultBg [3]float32 `toml:"default_bg"`
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

// Preset returns a fully-populated Config seeded from a named theme.
// Unknown or empty names fall back to rosepine, the default. The returned
// value is the baseline that TOML overrides are layered onto by Load.
func Preset(name string) Config {
	switch name {
	case "amber":
		return amberPreset()
	case "green":
		return greenPreset()
	default:
		return rosepinePreset()
	}
}

func amberPreset() Config {
	return Config{
		Theme: "amber",
		Font:  Font{Family: "", Size: 14},
		Atlas: Atlas{Scale: 4, Gamma: 1.0},
		Phosphor: Phosphor{
			Low:  [3]float32{0.35, 0.16, 0.0},
			High: [3]float32{1.0, 0.72, 0.1},
		},
		Blur:     Blur{Radius: 2.0, Strength: 0.5},
		Rounding: Rounding{Radius: 2.0},
		Cursor:   Cursor{Glow: 1.5, PulsePeriod: 0.9},
		Face:     Face{BgTint: 0.055, InsetShadow: 0.3},
		Contrast: Contrast{MinDelta: 0.35},
	}
}

func greenPreset() Config {
	return Config{
		Theme: "green",
		Font:  Font{Family: "", Size: 14},
		Atlas: Atlas{Scale: 4, Gamma: 1.0},
		Phosphor: Phosphor{
			Low:  [3]float32{0.0, 0.42, 0.30},
			High: [3]float32{0.0, 1.0, 0.78},
		},
		Blur:     Blur{Radius: 2.0, Strength: 0.5},
		Rounding: Rounding{Radius: 2.0},
		Cursor:   Cursor{Glow: 1.5, PulsePeriod: 0.9},
		Face:     Face{BgTint: 0.05, InsetShadow: 0.35},
		Contrast: Contrast{MinDelta: 0.35},
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
	base := [3]float32{0.098, 0.090, 0.141}
	text := [3]float32{0.878, 0.871, 0.957}
	iris := [3]float32{0.769, 0.655, 0.906}
	return Config{
		Theme:     "rosepine",
		TrueColor: true,
		Font:      Font{Family: "", Size: 14},
		Atlas:     Atlas{Scale: 4, Gamma: 1.0},
		Phosphor: Phosphor{
			Low:  base,
			High: iris,
		},
		Colors: Colors{
			DefaultFg: text,
			DefaultBg: base,
		},
		Blur:     Blur{Radius: 2.0, Strength: 0.5},
		Rounding: Rounding{Radius: 2.0},
		Cursor:   Cursor{Glow: 1.5, PulsePeriod: 0.9},
		Face:     Face{BgTint: 0.04, InsetShadow: 0.3},
		Contrast: Contrast{MinDelta: 0.35},
	}
}

// DefaultPath is where Load/Save operate unless told otherwise:
// $XDG_CONFIG_HOME/tubeless/config.toml (usually ~/.config).
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
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
