package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileReturnsPreset(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.toml"), "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Theme != "rosepine" {
		t.Fatalf("default theme = %q, want rosepine", cfg.Theme)
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
	cfg := Preset("amber")
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
	// Amber preset values…
	if cfg.Phosphor.Low != [3]float32{0.35, 0.16, 0.0} {
		t.Fatalf("phosphor low not from amber preset: %v", cfg.Phosphor.Low)
	}
	// …but the file's field wins.
	if cfg.Blur.Radius != 1.25 {
		t.Fatalf("radius = %v, want 1.25", cfg.Blur.Radius)
	}
	// Unmentioned field stays preset.
	if cfg.Blur.Strength != 0.5 {
		t.Fatalf("strength = %v, want preset 0.5", cfg.Blur.Strength)
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
	if cfg.Phosphor.Low != [3]float32{0.35, 0.16, 0.0} {
		t.Fatalf("phosphor low = %v, want amber preset", cfg.Phosphor.Low)
	}
}
