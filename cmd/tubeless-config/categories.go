package main

import (
	"fmt"
	"log"

	"github.com/moozd/tubeless/pkg/config"
	"github.com/moozd/tubeless/pkg/font"
)

// rowBuilder collects one category's rows. section/add are handed out as
// closures so each category reads as a flat list of settings under
// headings.
type rowBuilder struct{ list []panelRow }

func (r *rowBuilder) section(name string) {
	r.list = append(r.list, panelRow{kind: rowSection, section: name})
}

func (r *rowBuilder) add(s *setting) {
	r.list = append(r.list, panelRow{kind: rowSetting, set: s})
}

// buildCategories is the sidebar, top to bottom. Theme (colors) and
// Effects (everything the preset seeds) are separate on purpose: neither
// ever changes the other.
func buildCategories() []category {
	return []category{
		{name: "General", rows: generalRows()},
		{name: "Font", rows: fontRows()},
		{name: "Theme", rows: themeRows()},
		{name: "Effects", rows: effectsRows()},
		{name: "Cursor", rows: cursorRows()},
		{name: "CRT", rows: crtRows()},
	}
}

// systemFontItems lists installed font families, asking fontconfig once
// and remembering the answer for the rest of the session.
func systemFontItems() func() ([]string, error) {
	var names []string
	var err error
	loaded := false
	return func() ([]string, error) {
		if !loaded {
			names, err = font.SystemFamilies()
			if err != nil {
				log.Printf("list font families: %v", err)
			}
			loaded = true
		}
		return names, err
	}
}

func generalRows() []panelRow {
	var r rowBuilder
	section, add := r.section, r.add

	section("Layout")
	add(newSlider("padding.size", "padding", "empty margin kept around the terminal grid on every side", "px", 0, 1, 0, 200,
		func(c *config.Config) float64 { return float64(c.Padding.Size) },
		func(c *config.Config, v float64) { c.Padding.Size = float32(v) }))

	section("Shell")
	add(&setting{
		key: "shell.program", label: "shell",
		help: "which installed shell to launch into; \"auto\" follows $SHELL, \"tmux\" attaches to (or creates) a persistent session named \"home\" instead of a plain login shell — only applies when no --shell override is passed",
		get: func(c *config.Config) string {
			if c.Shell.Program == "" {
				return "auto"
			}
			return c.Shell.Program
		},
		applyStep: func(c *config.Config, d int) bool {
			current := c.Shell.Program
			if current == "" {
				current = "auto"
			}
			next := cycleName(shellChoiceNames(), current, d)
			if next == "auto" {
				next = ""
			}
			c.Shell.Program = next
			return false
		},
	})

	return r.list
}

func fontRows() []panelRow {
	var r rowBuilder
	section, add := r.section, r.add

	section("Typeface")
	add(&setting{
		key: "font.family", label: "family",
		help: "installed font family; enter to search, or type an exact name",
		get: func(c *config.Config) string {
			if c.Font.Family == "" {
				return "FiraCode Nerd Font"
			}
			return c.Font.Family
		},
		// Nothing to step through: enter opens the picker instead.
		applyStep: func(c *config.Config, d int) bool { return false },
		pick: &pickSpec{
			title:   "font family",
			items:   systemFontItems(),
			current: func(c *config.Config) string { return c.Font.Family },
			apply:   func(c *config.Config, v string) { c.Font.Family = v },
			free:    true,
			font:    true,
		},
	})
	add(&setting{
		key: "font.size", label: "size", help: "logical pixel height (rebuilds the atlas)",
		get: func(c *config.Config) string { return fmt.Sprintf("%d px", c.Font.Size) },
		applyStep: func(c *config.Config, d int) bool {
			c.Font.Size = clampInt(c.Font.Size+d, 10, 96)
			return true
		},
		rangeOf: func(c *config.Config) (float64, float64, float64) { return float64(c.Font.Size), 10, 96 },
	})
	add(&setting{
		key: "font.line_height", label: "line height", help: "line spacing multiplier (rebuilds the atlas)",
		get: func(c *config.Config) string { return trimFloat(c.Font.LineHeight, 2) },
		applyStep: func(c *config.Config, d int) bool {
			c.Font.LineHeight = roundFloat(clampFloat(c.Font.LineHeight+0.05*float64(d), 0.8, 2), 2)
			return true
		},
		rangeOf: func(c *config.Config) (float64, float64, float64) { return c.Font.LineHeight, 0.8, 2 },
	})
	add(&setting{
		key: "font.ligatures", label: "ligatures",
		help: "GSUB programming ligatures (=>, ->, !=, ...); rebuilds the atlas",
		get:  func(c *config.Config) string { return toggleStr(c.Font.Ligatures) },
		applyStep: func(c *config.Config, d int) bool {
			c.Font.Ligatures = d > 0
			return true
		},
	})
	add(&setting{
		key: "atlas.scale", label: "atlas scale", help: "glyph supersampling, 0 = auto per-monitor (rebuilds the atlas)",
		get: func(c *config.Config) string {
			if c.Atlas.Scale <= 0 {
				return "auto"
			}
			return fmt.Sprintf("%d×", c.Atlas.Scale)
		},
		applyStep: func(c *config.Config, d int) bool {
			c.Atlas.Scale = clampInt(c.Atlas.Scale+d, 0, 8)
			return true
		},
		rangeOf: func(c *config.Config) (float64, float64, float64) { return float64(c.Atlas.Scale), 0, 8 },
	})
	add(&setting{
		key: "font.gamma", label: "gamma", help: "coverage curve shaping (rebuilds the atlas)",
		get: func(c *config.Config) string { return trimFloat(c.Atlas.Gamma, 2) },
		applyStep: func(c *config.Config, d int) bool {
			c.Atlas.Gamma = roundFloat(clampFloat(c.Atlas.Gamma+0.05*float64(d), 0.5, 2), 2)
			return true
		},
		rangeOf: func(c *config.Config) (float64, float64, float64) { return c.Atlas.Gamma, 0.5, 2 },
	})

	return r.list
}

func themeRows() []panelRow {
	var r rowBuilder
	section, add := r.section, r.add

	section("Theme")
	add(&setting{
		key: "theme", label: "theme",
		help:  "colors only: the palette and phosphor ramp. Switching it never changes the effects preset",
		axis:  axisTheme,
		names: true,
		get:   func(c *config.Config) string { return c.Theme },
		applyStep: func(c *config.Config, d int) bool {
			applyThemeCfg(c, cycleName(config.ThemeNames(), c.Theme, d))
			return false
		},
		pick: &pickSpec{
			title:   "theme",
			items:   func() ([]string, error) { return config.ThemeNames(), nil },
			current: func(c *config.Config) string { return c.Theme },
			apply:   applyThemeCfg,
			preview: true,
		},
	})

	section("Custom colors")
	add(asThemeColor(newToggle("true_color", "true color",
		"off = the glow/accent phosphor ramp below drives every cell instead of real per-cell color",
		func(c *config.Config) bool { return c.TrueColor },
		func(c *config.Config, v bool) { c.TrueColor = v })))
	role := func(roleName, label string, get func(*config.Config) *[3]float32) {
		for _, ch := range "rgb" {
			add(asThemeColor(newThemeColorSlider(roleName, label, ch, get)))
		}
	}
	role("text", "text", func(c *config.Config) *[3]float32 { return &c.Colors.DefaultFg })
	role("bg", "background", func(c *config.Config) *[3]float32 { return &c.Colors.DefaultBg })
	role("accent", "accent", func(c *config.Config) *[3]float32 { return &c.Phosphor.High })
	role("glow", "glow", func(c *config.Config) *[3]float32 { return &c.Phosphor.Low })

	section("ANSI palette")
	for i := range 16 {
		add(asThemeColor(newPaletteSlot(i)))
	}

	section("Monochrome (true color off)")
	add(&setting{
		key: "monochrome.mode", label: "mode",
		help: "binary = a cell whose real colors land too close together always hard-inverts to solid black/phosphor-peak, guaranteed legible but flattens syntax highlighting to on/off blocks. shades = real colors quantize onto a handful of discrete accent-color brightness steps (see steps/hue weight below) instead, so syntax highlighting keeps reading as relative brightness while adjacent steps stay legible. spectrum = shades' brightness ladder mixed toward each cell's own real color (see amount below) — 0 is the plain ramp, 1 is the real color unmodified, so different colors read as genuinely different colors, not just different brightnesses.",
		get: func(c *config.Config) string {
			if c.Monochrome.Mode == "" {
				return "binary"
			}
			return c.Monochrome.Mode
		},
		applyStep: func(c *config.Config, d int) bool {
			current := c.Monochrome.Mode
			if current == "" {
				current = "binary"
			}
			c.Monochrome.Mode = cycleName(monochromeModeNames, current, d)
			return false
		},
	})
	add(&setting{
		key: "monochrome.steps", label: "shades/spectrum steps",
		help: "shades and spectrum modes only: how many discrete accent-color brightness levels real colors quantize onto — more steps keep finer distinctions but pack levels closer together.",
		get: func(c *config.Config) string {
			if c.Monochrome.Steps <= 0 {
				return "auto (16)"
			}
			return fmt.Sprintf("%d", c.Monochrome.Steps)
		},
		applyStep: func(c *config.Config, d int) bool {
			cur := c.Monochrome.Steps
			if cur <= 0 {
				cur = 16
			}
			c.Monochrome.Steps = clampInt(cur+d, 2, 24)
			return false
		},
		rangeOf: func(c *config.Config) (float64, float64, float64) {
			cur := c.Monochrome.Steps
			if cur <= 0 {
				cur = 16
			}
			return float64(cur), 2, 24
		},
	})
	add(&setting{
		key: "monochrome.hue_weight", label: "shades hue weight",
		help: "shades mode only: how much brightness leans on a color's closeness to the theme's own accent hue, on top of its real luminance — 0 is luminance only, 1 is hue-closeness only.",
		get: func(c *config.Config) string {
			if c.Monochrome.HueWeight < 0 {
				return "auto (0.6)"
			}
			return trimFloat(float64(c.Monochrome.HueWeight), 2)
		},
		applyStep: func(c *config.Config, d int) bool {
			cur := float64(c.Monochrome.HueWeight)
			if cur < 0 {
				cur = 0.6
			}
			c.Monochrome.HueWeight = float32(roundFloat(clampFloat(cur+0.05*float64(d), 0, 1), 2))
			return false
		},
		rangeOf: func(c *config.Config) (float64, float64, float64) {
			cur := float64(c.Monochrome.HueWeight)
			if cur < 0 {
				cur = 0.6
			}
			return cur, 0, 1
		},
	})
	add(&setting{
		key: "monochrome.amount", label: "spectrum amount",
		help: "spectrum mode only: how much a cell's rendered color mixes toward its own real color — 0 stays on the plain phosphor ramp (same as shades), 1 is the real color unmodified, same as true color.",
		get: func(c *config.Config) string {
			if c.Monochrome.Amount < 0 {
				return "auto (0.4)"
			}
			return trimFloat(float64(c.Monochrome.Amount), 2)
		},
		applyStep: func(c *config.Config, d int) bool {
			cur := float64(c.Monochrome.Amount)
			if cur < 0 {
				cur = 0.4
			}
			c.Monochrome.Amount = float32(roundFloat(clampFloat(cur+0.05*float64(d), 0, 1), 2))
			return false
		},
		rangeOf: func(c *config.Config) (float64, float64, float64) {
			cur := float64(c.Monochrome.Amount)
			if cur < 0 {
				cur = 0.4
			}
			return cur, 0, 1
		},
	})

	return r.list
}

func effectsRows() []panelRow {
	var r rowBuilder
	section, add := r.section, r.add

	section("Preset")
	add(&setting{
		key: "preset", label: "preset",
		help:     "effects only: surface, bloom, glow, cursor and CRT values. Switching it never changes the theme",
		axis:     axisEffects,
		names:    true,
		noMarker: true,
		get:      func(c *config.Config) string { return c.Preset },
		applyStep: func(c *config.Config, d int) bool {
			applyEffectsPresetCfg(c, cycleName(config.EffectsPresetNames(), c.Preset, d))
			return false
		},
		pick: &pickSpec{
			title:   "effects preset",
			items:   func() ([]string, error) { return config.EffectsPresetNames(), nil },
			current: func(c *config.Config) string { return c.Preset },
			apply:   applyEffectsPresetCfg,
			preview: true,
		},
	})

	section("Surface")
	add(asEffect(newSlider("surface.radius", "corner radius", "fragment-space radius for block/border pixels; text stays literal", "px", 1, 0.1, 0, 8,
		func(c *config.Config) float64 { return float64(c.Surface.Radius) },
		func(c *config.Config, v float64) { c.Surface.Radius = float32(v) })))
	add(asEffect(newSlider("surface.gradient", "gradient sheen", "top-lit/bottom-shaded gradient over each block, from its own color", "", 2, 0.01, 0, 1,
		func(c *config.Config) float64 { return float64(c.Surface.Gradient) },
		func(c *config.Config, v float64) { c.Surface.Gradient = float32(v) })))
	add(asEffect(newSlider("surface.shadow", "drop shadow", "soft layered shadow cast down-right of each block, matching its rounded corners", "", 2, 0.01, 0, 1,
		func(c *config.Config) float64 { return float64(c.Surface.Shadow) },
		func(c *config.Config, v float64) { c.Surface.Shadow = float32(v) })))

	section("Bloom")
	add(asEffect(newSlider("blur.radius", "glow radius", "gaussian spread in px on block/border pixels; text stays sharp", "px", 1, 0.1, 0, 8,
		func(c *config.Config) float64 { return float64(c.Blur.Radius) },
		func(c *config.Config, v float64) { c.Blur.Radius = float32(v) })))
	add(asEffect(newSlider("blur.strength", "glow strength", "strength of the post-process glow under images/underlines/text", "", 2, 0.01, 0, 1,
		func(c *config.Config) float64 { return float64(c.Blur.Strength) },
		func(c *config.Config, v float64) { c.Blur.Strength = float32(v) })))

	section("Text glow")
	add(asEffect(newSlider("text_glow.radius", "glow radius", "gaussian spread in px of the halo around colorful text", "px", 1, 0.1, 0, 8,
		func(c *config.Config) float64 { return float64(c.TextGlow.Radius) },
		func(c *config.Config, v float64) { c.TextGlow.Radius = float32(v) })))
	add(asEffect(newSlider("text_glow.strength", "glow strength", "intensity of the halo, in each glyph's own color; 0 is off", "", 2, 0.01, 0, 1,
		func(c *config.Config) float64 { return float64(c.TextGlow.Strength) },
		func(c *config.Config, v float64) { c.TextGlow.Strength = float32(v) })))
	add(asEffect(newSlider("text_glow.threshold", "threshold", "how colorful a glyph must be to glow: 0 glows all text, higher keeps neutral text crisp", "", 2, 0.01, 0, 1,
		func(c *config.Config) float64 { return float64(c.TextGlow.Threshold) },
		func(c *config.Config, v float64) { c.TextGlow.Threshold = float32(v) })))

	section("Tube face")
	add(asEffect(newSlider("face.bg_tint", "bg tint", "brightness of the unlit screen", "", 3, 0.005, 0, 0.5,
		func(c *config.Config) float64 { return float64(c.Face.BgTint) },
		func(c *config.Config, v float64) { c.Face.BgTint = float32(v) })))
	add(asEffect(newSlider("face.inset_shadow", "inset shadow", "radial falloff to the corners", "", 2, 0.01, 0, 1,
		func(c *config.Config) float64 { return float64(c.Face.InsetShadow) },
		func(c *config.Config, v float64) { c.Face.InsetShadow = float32(v) })))

	section("Contrast")
	add(asEffect(newSlider("contrast.min_delta", "min contrast", "minimum fg/bg gap on the mono ramp", "", 2, 0.01, 0, 1,
		func(c *config.Config) float64 { return float64(c.Contrast.MinDelta) },
		func(c *config.Config, v float64) { c.Contrast.MinDelta = float32(v) })))

	return r.list
}

func cursorRows() []panelRow {
	var r rowBuilder
	section, add := r.section, r.add

	section("Appearance")
	add(asEffect(&setting{
		key: "cursor.shape", label: "shape",
		help: "at-rest outline; the speed-reactive ball/tail morph layers on top of whichever is picked",
		get:  func(c *config.Config) string { return c.Cursor.Shape },
		applyStep: func(c *config.Config, d int) bool {
			c.Cursor.Shape = cycleName(cursorShapeNames, c.Cursor.Shape, d)
			return false
		},
	}))
	add(asEffect(newSlider("cursor.radius", "corner radius", "at-rest corner rounding, as a fraction of the shape's short half-dimension", "", 2, 0.05, 0, 1,
		func(c *config.Config) float64 { return float64(c.Cursor.Radius) },
		func(c *config.Config, v float64) { c.Cursor.Radius = float32(v) })))
	add(asEffect(newSlider("cursor.glow", "glow", "halo width of the cursor", "px", 1, 0.1, 0, 8,
		func(c *config.Config) float64 { return float64(c.Cursor.Glow) },
		func(c *config.Config, v float64) { c.Cursor.Glow = float32(v) })))
	add(asEffect(&setting{
		key: "cursor.blink_style", label: "blink style",
		help: "how the cursor's brightness pulses over time; forced to static whenever glass mode is on",
		get:  func(c *config.Config) string { return c.Cursor.BlinkStyle },
		applyStep: func(c *config.Config, d int) bool {
			c.Cursor.BlinkStyle = cycleName(cursorBlinkStyleNames, c.Cursor.BlinkStyle, d)
			return false
		},
	}))
	add(asEffect(newSlider("cursor.pulse_period", "pulse period", "breathing/toggle period of the cursor", "s", 2, 0.05, 0.2, 4,
		func(c *config.Config) float64 { return float64(c.Cursor.PulsePeriod) },
		func(c *config.Config, v float64) { c.Cursor.PulsePeriod = float32(v) })))

	section("Trail")
	add(asEffect(newToggle("cursor.trail.enabled", "enabled",
		"morphs the cursor into a ball with a tail during a fast glide (a jump across the buffer, not ordinary typing), then eases back to its at-rest shape — mirrors neovide's cursor trail",
		func(c *config.Config) bool { return c.Cursor.Trail.Enabled },
		func(c *config.Config, v bool) { c.Cursor.Trail.Enabled = v })))
	add(asEffect(newSlider("cursor.trail.size", "size", "how far the tail reaches at full speed, 0 (no tail) .. 1 (longest) — like neovide's cursor_trail_size", "", 2, 0.05, 0, 1,
		func(c *config.Config) float64 { return float64(c.Cursor.Trail.Size) },
		func(c *config.Config, v float64) { c.Cursor.Trail.Size = float32(v) })))
	add(asEffect(newSlider("cursor.trail.length", "length", "how long the morph itself takes to catch up to its shape — like neovide's cursor_animation_length", "s", 2, 0.01, 0.01, 1,
		func(c *config.Config) float64 { return float64(c.Cursor.Trail.Length) },
		func(c *config.Config, v float64) { c.Cursor.Trail.Length = float32(v) })))

	section("Glass (experimental)")
	add(asEffect(newToggle("cursor.glass.enabled", "enabled",
		"macOS-style frosted panel that refracts/blurs the scene behind the cursor instead of glowing over it; forces blink style to static",
		func(c *config.Config) bool { return c.Cursor.Glass.Enabled },
		func(c *config.Config, v bool) { c.Cursor.Glass.Enabled = v })))
	add(asEffect(newSlider("cursor.glass.tint", "tint", "accent strength mixed into the refracted sample", "", 2, 0.02, 0, 1,
		func(c *config.Config) float64 { return float64(c.Cursor.Glass.Tint) },
		func(c *config.Config, v float64) { c.Cursor.Glass.Tint = float32(v) })))
	add(asEffect(newSlider("cursor.glass.blur", "blur", "refraction sample blur spread", "px", 1, 0.1, 0, 6,
		func(c *config.Config) float64 { return float64(c.Cursor.Glass.Blur) },
		func(c *config.Config, v float64) { c.Cursor.Glass.Blur = float32(v) })))
	add(asEffect(newSlider("cursor.glass.refract", "refract", "outward bend of the sampled scene", "px", 1, 0.25, 0, 12,
		func(c *config.Config) float64 { return float64(c.Cursor.Glass.Refract) },
		func(c *config.Config, v float64) { c.Cursor.Glass.Refract = float32(v) })))
	add(asEffect(newSlider("cursor.glass.opacity", "opacity", "core opacity of the glass panel", "", 2, 0.02, 0, 1,
		func(c *config.Config) float64 { return float64(c.Cursor.Glass.Opacity) },
		func(c *config.Config, v float64) { c.Cursor.Glass.Opacity = float32(v) })))

	return r.list
}

func crtRows() []panelRow {
	var r rowBuilder
	section, add := r.section, r.add

	// Every CRT effect below is off by default (0) in the modern
	// preset and independently configurable — see pkg/config/crt.go's
	// doc comment for why these are plain floats rather than a separate
	// Enabled flag.
	section("Curvature")
	add(asEffect(newSlider("crt.curvature.amount", "amount", "barrel-distortion strength; 0 = flat", "", 2, 0.01, 0, 0.5,
		func(c *config.Config) float64 { return float64(c.CRT.Curvature.Amount) },
		func(c *config.Config, v float64) { c.CRT.Curvature.Amount = float32(v) })))

	section("Scanlines")
	add(asEffect(newSlider("crt.scanlines.intensity", "intensity", "darkens alternating lines; 0 = off", "", 2, 0.01, 0, 1,
		func(c *config.Config) float64 { return float64(c.CRT.Scanlines.Intensity) },
		func(c *config.Config, v float64) { c.CRT.Scanlines.Intensity = float32(v) })))
	add(asEffect(newSlider("crt.scanlines.period", "period", "device px per line-pair", "px", 1, 0.5, 1, 12,
		func(c *config.Config) float64 { return float64(c.CRT.Scanlines.Period) },
		func(c *config.Config, v float64) { c.CRT.Scanlines.Period = float32(v) })))

	section("Chromatic aberration")
	add(asEffect(newSlider("crt.aberration.amount", "amount", "red/blue channel offset; 0 = off", "", 4, 0.0005, 0, 0.02,
		func(c *config.Config) float64 { return float64(c.CRT.Aberration.Amount) },
		func(c *config.Config, v float64) { c.CRT.Aberration.Amount = float32(v) })))

	section("Shadow mask")
	add(asEffect(newSlider("crt.shadow_mask.intensity", "intensity", "RGB triad overlay strength; 0 = off", "", 2, 0.01, 0, 1,
		func(c *config.Config) float64 { return float64(c.CRT.ShadowMask.Intensity) },
		func(c *config.Config, v float64) { c.CRT.ShadowMask.Intensity = float32(v) })))
	add(asEffect(newSlider("crt.shadow_mask.cell_size", "cell size", "device px per triad column", "px", 1, 0.5, 1, 12,
		func(c *config.Config) float64 { return float64(c.CRT.ShadowMask.CellSize) },
		func(c *config.Config, v float64) { c.CRT.ShadowMask.CellSize = float32(v) })))

	section("Pixel grid")
	add(asEffect(newSlider("crt.pixel_grid.intensity", "intensity", "dark gaps between pixel cells; 0 = off", "", 2, 0.01, 0, 1,
		func(c *config.Config) float64 { return float64(c.CRT.PixelGrid.Intensity) },
		func(c *config.Config, v float64) { c.CRT.PixelGrid.Intensity = float32(v) })))
	add(asEffect(newSlider("crt.pixel_grid.cell_size", "cell size", "device px per pixel cell", "px", 1, 0.5, 2, 12,
		func(c *config.Config) float64 { return float64(c.CRT.PixelGrid.CellSize) },
		func(c *config.Config, v float64) { c.CRT.PixelGrid.CellSize = float32(v) })))
	add(asEffect(newSlider("crt.pixel_grid.gap", "gap", "border width as a fraction of a cell", "", 2, 0.01, 0, 0.5,
		func(c *config.Config) float64 { return float64(c.CRT.PixelGrid.Gap) },
		func(c *config.Config, v float64) { c.CRT.PixelGrid.Gap = float32(v) })))

	section("Noise")
	add(asEffect(newSlider("crt.noise.intensity", "intensity", "analog signal noise; 0 = off", "", 3, 0.005, 0, 0.2,
		func(c *config.Config) float64 { return float64(c.CRT.Noise.Intensity) },
		func(c *config.Config, v float64) { c.CRT.Noise.Intensity = float32(v) })))

	section("Flicker")
	add(asEffect(newSlider("crt.flicker.amount", "amount", "whole-screen brightness jitter; 0 = off", "", 2, 0.01, 0, 1,
		func(c *config.Config) float64 { return float64(c.CRT.Flicker.Amount) },
		func(c *config.Config, v float64) { c.CRT.Flicker.Amount = float32(v) })))
	add(asEffect(newSlider("crt.flicker.speed", "speed", "flicker rate", "hz", 1, 0.5, 0.5, 30,
		func(c *config.Config) float64 { return float64(c.CRT.Flicker.Speed) },
		func(c *config.Config, v float64) { c.CRT.Flicker.Speed = float32(v) })))

	section("Phosphor decay")
	add(asEffect(newSlider("crt.phosphor_decay.decay_seconds", "decay seconds", "afterglow trail length; 0 = off", "s", 2, 0.05, 0, 3,
		func(c *config.Config) float64 { return float64(c.CRT.PhosphorDecay.DecaySeconds) },
		func(c *config.Config, v float64) { c.CRT.PhosphorDecay.DecaySeconds = float32(v) })))

	section("Aspect ratio")
	add(asEffect(newSlider("crt.aspect_ratio.width", "width", "letterbox/pillarbox to width:height instead of filling the window; 0 (either field) = off, full window", "", 0, 1, 0, 1200,
		func(c *config.Config) float64 { return float64(c.CRT.AspectRatio.Width) },
		func(c *config.Config, v float64) { c.CRT.AspectRatio.Width = float32(v) })))
	add(asEffect(newSlider("crt.aspect_ratio.height", "height", "paired with width above; 0 (either field) = off, full window", "", 0, 1, 0, 1200,
		func(c *config.Config) float64 { return float64(c.CRT.AspectRatio.Height) },
		func(c *config.Config, v float64) { c.CRT.AspectRatio.Height = float32(v) })))

	return r.list
}
