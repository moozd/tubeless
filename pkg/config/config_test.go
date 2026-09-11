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

func TestLoadMissingFileReturnsPreset(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.toml"), "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Theme != "rosepine" {
		t.Fatalf("default theme = %q, want rosepine", cfg.Theme)
	}
	if cfg.Preset != "modern" {
		t.Fatalf("default preset = %q, want modern", cfg.Preset)
	}
	if !cfg.TrueColor {
		t.Fatalf("default theme should be TrueColor")
	}
	if cfg.Font.Size != 14 || cfg.Atlas.Scale != 4 {
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
	// Unmentioned field stays the modern preset's.
	if cfg.Blur.Strength != 0.5 {
		t.Fatalf("strength = %v, want modern 0.5", cfg.Blur.Strength)
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
	monochrome := map[string]bool{"green": true, "amber": true, "green-p39": true, "white-p4": true}
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
			if i == 0 && name == "cga" {
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

// TestMonitorThemeLinksResolve confirms every effects preset MonitorTheme
// links to is a real, populated theme — catching a typo'd theme name in
// either monitors.go table before it ships.
func TestMonitorThemeLinksResolve(t *testing.T) {
	names := map[string]bool{}
	for _, n := range ThemeNames() {
		names[n] = true
	}
	for _, preset := range EffectsPresetNames() {
		theme, ok := MonitorTheme(preset)
		if !ok {
			continue
		}
		if !names[theme] {
			t.Errorf("MonitorTheme(%q) = %q, not in ThemeNames()", preset, theme)
		}
	}
}
