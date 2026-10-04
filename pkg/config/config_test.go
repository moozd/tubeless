package config

import (
	"os"
	"path/filepath"
	"testing"
)

// themedConfig assembles a Config the way Load would for a config.toml
// containing only `theme = "<name>"` — Default()'s effects axis (the
// modern preset) plus the named theme's color axis.
func themedConfig(theme string) Config {
	cfg := Default()
	applyTheme(&cfg, theme)
	return cfg
}

// TestDefaultSeedsHueWeightSentinel guards a real footgun: HueWeight's
// Go zero value (0) is itself a valid explicit "luminance only" choice
// (see Monochrome's own doc comment), so it can't double as "unset" —
// Default() must seed the -1 sentinel explicitly, or every fresh
// install would silently behave as if hue_weight were pinned to 0
// instead of getting the built-in default.
func TestDefaultSeedsHueWeightSentinel(t *testing.T) {
	if got := Default().Monochrome.HueWeight; got != -1 {
		t.Fatalf("Default().Monochrome.HueWeight = %v, want -1 (unset sentinel)", got)
	}
}

// TestDefaultSeedsAmountSentinel is Amount's counterpart to the guard
// above: its Go zero value (0) is itself a valid explicit "stay on the
// ramp" choice (see Monochrome's own doc comment), so Default() must
// seed the -1 sentinel explicitly too.
func TestDefaultSeedsAmountSentinel(t *testing.T) {
	if got := Default().Monochrome.Amount; got != -1 {
		t.Fatalf("Default().Monochrome.Amount = %v, want -1 (unset sentinel)", got)
	}
}

func TestLoadMissingFileReturnsPreset(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.toml"), "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Theme != "rosepine" {
		t.Fatalf("default theme = %q, want rosepine", cfg.Theme)
	}
	if cfg.Preset != "custom" {
		t.Fatalf("default preset = %q, want custom", cfg.Preset)
	}
	if !cfg.TrueColor {
		t.Fatalf("default theme should be TrueColor")
	}
	if cfg.Font.Size != 14 || cfg.Atlas.Scale != 1 {
		t.Fatalf("unexpected defaults: %+v", cfg.Font)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tubeless", "config.toml")
	cfg := themedConfig("amber")
	cfg.Blur.Radius = 3.5
	cfg.Phosphor.High = [3]float32{0.9, 0.1, 0.2}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Theme != "amber" {
		t.Fatalf("theme = %q, want amber", got.Theme)
	}
	if got.Blur.Radius != 3.5 {
		t.Fatalf("blur radius = %v, want 3.5", got.Blur.Radius)
	}
	if got.Phosphor.High != [3]float32{0.9, 0.1, 0.2} {
		t.Fatalf("phosphor high = %v", got.Phosphor.High)
	}
}

func TestLoadFileOverridesPresetPerField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.toml")
	if err := os.WriteFile(path, []byte("theme = \"amber\"\n[blur]\nradius = 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Amber theme's phosphor color (see monitors.go/config.go) — derived,
	// not a literal, so compare against the theme function itself.
	if want := amberTheme().Phosphor.Low; cfg.Phosphor.Low != want {
		t.Fatalf("phosphor low not from amber theme: %v, want %v", cfg.Phosphor.Low, want)
	}
	// …but the file's field wins.
	if cfg.Blur.Radius != 1.25 {
		t.Fatalf("radius = %v, want 1.25", cfg.Blur.Radius)
	}
	// Unmentioned field stays Default()'s own (no preset named in the
	// file, so it's never reseeded away from the custom default).
	if cfg.Blur.Strength != 0.4 {
		t.Fatalf("strength = %v, want default 0.4", cfg.Blur.Strength)
	}
}

func TestFlagThemeWinsOverFileTheme(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.toml")
	if err := os.WriteFile(path, []byte("theme = \"green\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, "amber")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := amberTheme().Phosphor.Low; cfg.Phosphor.Low != want {
		t.Fatalf("phosphor low = %v, want amber theme %v", cfg.Phosphor.Low, want)
	}
}

func TestFilePresetOverridesEffectsPerField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.toml")
	if err := os.WriteFile(path, []byte("preset = \"ibm-5151\"\n[blur]\nradius = 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Preset != "ibm-5151" {
		t.Fatalf("preset = %q, want ibm-5151", cfg.Preset)
	}
	if want := ibm5151Effects().CRT.PhosphorDecay.DecaySeconds; cfg.CRT.PhosphorDecay.DecaySeconds != want {
		t.Fatalf("phosphor decay not from ibm-5151 preset: %v, want %v", cfg.CRT.PhosphorDecay.DecaySeconds, want)
	}
	if cfg.Blur.Radius != 1.25 {
		t.Fatalf("radius = %v, want 1.25", cfg.Blur.Radius)
	}
	// Theme is untouched by a preset-only file — no theme key given.
	if cfg.Theme != "rosepine" {
		t.Fatalf("theme = %q, want unaffected rosepine default", cfg.Theme)
	}
}

// TestThemesPopulated catches the likely copy-paste mistake across the
// near-identical TrueColor theme functions: a theme that forgot to set
// TrueColor, or whose Colors.Palette still has an all-zero (black) slot
// because a hex triple was dropped while transcribing the theme's
// published palette.
func TestThemesPopulated(t *testing.T) {
	monochrome := map[string]bool{"green": true, "amber": true, "green-p39": true, "white-p4": true, "cyberpunk": true,
		"nixie": true, "vfd": true, "dmg": true, "scope": true, "ember": true, "sepia": true, "ultraviolet": true,
		"rose-gold": true, "seafoam": true, "lavender-haze": true, "sunset": true, "sage": true,
		"glacier": true, "mulberry": true, "gold-leaf": true,
		"green-night": true, "moss": true, "emerald-noir": true, "amber-night": true, "bronze-night": true,
		"ice-night": true, "p4-night": true, "crimson-night": true,
		"pipboy": true, "nostromo": true, "wopr": true, "matrix": true, "replicant": true, "tron": true,
		"synthwave": true, "c64": true, "terminator": true, "vertigo": true, "ibm-3278": true,
		"arcade": true, "radar-p7": true}
	for _, name := range ThemeNames() {
		tc := Theme(name)
		if monochrome[name] {
			if tc.Phosphor.High == ([3]float32{}) {
				t.Errorf("Theme(%q).Phosphor.High is all-zero", name)
			}
			continue
		}
		if !tc.TrueColor {
			t.Errorf("Theme(%q).TrueColor = false, want true", name)
		}
		for i, c := range tc.Colors.Palette {
			// cga's index 0 is authentically black (see cgaTheme) — a
			// real all-zero slot, not a dropped hex triple.
			if i == 0 && (name == "cga" || name == "msdos-blue" || name == "msdos-black" || name == "turbo-blue") {
				continue
			}
			if c == ([3]float32{}) {
				t.Errorf("Theme(%q).Colors.Palette[%d] is all-zero", name, i)
			}
		}
	}
}

// TestEffectsPresetsNamed catches an effects preset whose fields got left
// at every field's Go zero value by mistake — modern legitimately
// zeroes the whole CRT struct (every CRT effect is off by design), so
// this checks the non-CRT chrome fields every preset (monitor or not)
// always sets to something nonzero instead.
func TestEffectsPresetsNamed(t *testing.T) {
	for _, name := range EffectsPresetNames() {
		e := EffectsPreset(name)
		if e.Blur.Radius == 0 && e.Blur.Strength == 0 {
			t.Errorf("EffectsPreset(%q).Blur is all-zero", name)
		}
		if e.Cursor.Glow == 0 {
			t.Errorf("EffectsPreset(%q).Cursor.Glow is zero", name)
		}
	}
}

// TestEveryListedNameIsRegistered catches a name added to ThemeNames or
// EffectsPresetNames but not to the switch behind it, which would
// silently fall back to the default look.
func TestEveryListedNameIsRegistered(t *testing.T) {
	for _, name := range ThemeNames() {
		if name != "rosepine" && Theme(name) == Theme("rosepine") {
			t.Errorf("theme %q falls back to rosepine", name)
		}
	}
	for _, name := range EffectsPresetNames() {
		if name != "modern" && EffectsPreset(name) == EffectsPreset("modern") {
			t.Errorf("preset %q falls back to modern", name)
		}
	}
}

// TestNeonGlowIsSelective guards the neon look's core idea: its
// TextGlow threshold must sit above every neutral color the theme
// draws plain text in, and below every one of its neon hues — or
// either nothing glows or everything does.
func TestNeonGlowIsSelective(t *testing.T) {
	threshold := EffectsPreset("neon").TextGlow.Threshold
	full := threshold + 0.25
	c := Theme("neon").Colors
	neutral := [][3]float32{c.DefaultFg, c.Palette[0], c.Palette[7], c.Palette[8], c.Palette[15]}
	for i, col := range neutral {
		if got := colorfulness(col); got >= threshold {
			t.Errorf("neutral color %d colorfulness %.2f glows (threshold %.2f)", i, got, threshold)
		}
	}
	for _, i := range []int{1, 2, 3, 4, 5, 6, 9, 10, 11, 12, 13, 14} {
		if got := colorfulness(c.Palette[i]); got < full {
			t.Errorf("palette %d colorfulness %.2f is below full glow at %.2f", i, got, full)
		}
	}
}

// colorfulness mirrors cell_glyph.frag's glow weight input: linear RGB
// max minus min.
func colorfulness(c [3]float32) float32 {
	return max(c[0], c[1], c[2]) - min(c[0], c[1], c[2])
}

// TestDailyDriverIsTheAuthorsConfig pins the daily-driver preset to the
// exact values of the author's own config, so it stays that look and not
// "close to it".
func TestDailyDriverIsTheAuthorsConfig(t *testing.T) {
	e := EffectsPreset("daily-driver")
	if e.Surface != (Surface{Radius: 1.7}) || e.Blur != (Blur{Radius: 2.5, Strength: 0.6}) {
		t.Errorf("surface/blur drifted: %+v %+v", e.Surface, e.Blur)
	}
	if e.Face != (Face{BgTint: 0.06, InsetShadow: 0.48}) || e.Contrast.MinDelta != 0.35 {
		t.Errorf("face/contrast drifted: %+v %+v", e.Face, e.Contrast)
	}
	if e.CRT != (CRT{Scanlines: Scanlines{Intensity: 0.45, Period: 3.5}}) {
		t.Errorf("crt drifted: %+v", e.CRT)
	}
	c := e.Cursor
	if c.Glow != 1.5 || c.PulsePeriod != 0.9 || c.Radius != 0.2 || c.Shape != "block" || c.BlinkStyle != "ease" ||
		c.Trail != (Trail{Enabled: true, Size: 0.5, Length: 0.13}) {
		t.Errorf("cursor drifted: %+v", c)
	}
}

// TestMSDOSThemesUseTheVGAPalette pins the DOS themes to the real VGA
// text palette and their blue/black backgrounds.
func TestMSDOSThemesUseTheVGAPalette(t *testing.T) {
	blue, black := Theme("msdos-blue"), Theme("msdos-black")
	if !blue.TrueColor || !black.TrueColor {
		t.Fatal("DOS themes are true-color")
	}
	if blue.Colors.Palette != vgaPalette() || black.Colors.Palette != vgaPalette() {
		t.Error("DOS themes must use the VGA palette")
	}
	if blue.Colors.DefaultBg != srgb3(0, 0, 0xaa) || black.Colors.DefaultBg != srgb3(0, 0, 0) {
		t.Error("DOS backgrounds are VGA blue and black")
	}
	if blue.Colors.DefaultFg != srgb3(0xaa, 0xaa, 0xaa) {
		t.Error("DOS text is VGA light gray")
	}
}

// TestNightThemesStayDim checks the night palettes earn their name: no
// bright end above 80%.
func TestNightThemesStayDim(t *testing.T) {
	for _, name := range []string{"green-night", "moss", "emerald-noir", "amber-night", "bronze-night", "ice-night", "p4-night", "crimson-night"} {
		high := Theme(name).Phosphor.High
		if peak := max(high[0], high[1], high[2]); peak > 0.8 {
			t.Errorf("%s text peaks at %.2f, want at most 0.8", name, peak)
		}
	}
}

// TestPresetNeverTouchesTheme pins the two axes as independent: loading
// a config that names only a preset leaves the default theme's colors
// untouched, whichever preset it is.
func TestPresetNeverTouchesTheme(t *testing.T) {
	want := themedConfig("rosepine")
	for _, name := range EffectsPresetNames() {
		cfg := themedConfig("rosepine")
		applyEffectsPreset(&cfg, name)
		if cfg.Theme != want.Theme || cfg.Colors != want.Colors || cfg.Phosphor != want.Phosphor {
			t.Errorf("preset %q changed the theme axis", name)
		}
	}
}

// TestUnknownNamesBecomeCustom keeps a config naming a removed preset or
// theme from silently wearing a name it no longer matches.
func TestUnknownNamesBecomeCustom(t *testing.T) {
	cfg := Default()
	applyEffectsPreset(&cfg, "nixie")
	applyTheme(&cfg, "no-such-theme")
	if cfg.Preset != "custom" || cfg.Theme != "custom" {
		t.Errorf("got preset %q theme %q, want custom for both", cfg.Preset, cfg.Theme)
	}
}
