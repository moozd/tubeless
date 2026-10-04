package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/moozd/tubeless/pkg/config"
)

// cursorShapeNames/cursorBlinkStyleNames back the cursor.shape and
// cursor.blink_style cycler rows above — see config.Cursor's own doc
// comment for what each name means.
var (
	cursorShapeNames      = []string{"block", "bar", "underline"}
	cursorBlinkStyleNames = []string{"ease", "static", "hard"}
	// monochromeModeNames backs the monochrome.mode cycler row — see
	// config.Monochrome's own doc comment for what each name means.
	monochromeModeNames = []string{"binary", "shades", "spectrum"}
)

func cycleName(names []string, current string, d int) string {
	idx := slices.Index(names, current)
	if idx < 0 {
		idx = 0
	} else {
		n := len(names)
		idx = ((idx+d)%n + n) % n
	}
	return names[idx]
}

// shellWellKnownNames are common shell binary names checked directly on
// PATH — a backstop for shells (fish, nu, elvish, xonsh, pwsh) that don't
// always register themselves in /etc/shells the way the POSIX-standard
// ones do.
var shellWellKnownNames = []string{
	"bash", "zsh", "fish", "dash", "ksh", "tcsh", "csh", "sh",
	"nu", "elvish", "xonsh", "pwsh",
}

// shellChoiceNames lists every shell this machine actually has installed,
// for the shell.program cycler row — "auto" ($SHELL) always first, then
// every distinct shell name found either in /etc/shells (the
// POSIX-standard list of approved login shells, filtered to entries that
// still exist — some systems leave stale ones behind after uninstalling
// a package) or under a well-known name resolved via PATH
// (shellWellKnownNames), sorted; "tmux" is appended last, only when it's
// actually on PATH, as a distinct non-shell option — see
// cmd/tubeless's tmuxCommand for what selecting it does. Scans the
// filesystem/PATH fresh on every call rather than caching: this only
// runs when the settings screen is actually rendering this row, cheap
// enough not to matter, and correctly reflects a shell installed/removed
// while the config TUI is open.
func shellChoiceNames() []string {
	seen := map[string]bool{"auto": true}
	names := []string{"auto"}
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	if data, err := os.ReadFile("/etc/shells"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if _, err := os.Stat(line); err == nil {
				add(filepath.Base(line))
			}
		}
	}
	for _, name := range shellWellKnownNames {
		if _, err := exec.LookPath(name); err == nil {
			add(name)
		}
	}
	sort.Strings(names[1:])
	if _, err := exec.LookPath("tmux"); err == nil {
		names = append(names, "tmux")
	}
	return names
}

// applyEffectsPresetCfg seeds c's effects axis from a named preset. It
// never touches the theme: colors and effects are chosen independently.
func applyEffectsPresetCfg(c *config.Config, name string) {
	e := config.EffectsPreset(name)
	c.Preset = name
	c.Surface, c.Blur, c.Cursor = e.Surface, e.Blur, e.Cursor
	c.TextGlow = e.TextGlow
	c.Face, c.Contrast, c.CRT = e.Face, e.Contrast, e.CRT
}

// applyThemeCfg seeds c's color axis from a named theme.
func applyThemeCfg(c *config.Config, name string) {
	t := config.Theme(name)
	c.Theme = name
	c.TrueColor, c.Phosphor, c.Colors = t.TrueColor, t.Phosphor, t.Colors
}

// asEffect/asThemeColor put a setting on the effects/theme axis (see
// adjust(): hand-editing it flips that axis's name to "custom") and
// return it, so a row can be built and marked in one
// add(asEffect(newSlider(...))) call.
func asEffect(s *setting) *setting     { s.axis = axisEffects; return s }
func asThemeColor(s *setting) *setting { s.axis = axisTheme; s.compact = true; return s }

// newSlider builds a numeric setting: get/put close over one Config
// field, step adjusts it by ±step per arrow press (clamped to
// [min,max]), and rangeOf exposes the same (value, min, max) for the
// selected row's meter.
func newSlider(key, label, help, unit string, dec int, step, min, max float64,
	get func(*config.Config) float64, put func(*config.Config, float64)) *setting {
	return &setting{
		key: key, label: label, help: help,
		get: func(c *config.Config) string { return trimFloat(get(c), dec) + unit },
		applyStep: func(c *config.Config, d int) bool {
			put(c, roundFloat(clampFloat(get(c)+step*float64(d), min, max), dec))
			return false
		},
		rangeOf: func(c *config.Config) (float64, float64, float64) { return get(c), min, max },
	}
}

// newToggle builds a boolean setting: right/left arrow sets it on/off,
// displayed as a filled/hollow circle (see toggleStr) rather than plain
// on/off text.
func newToggle(key, label, help string, get func(*config.Config) bool, put func(*config.Config, bool)) *setting {
	return &setting{
		key: key, label: label, help: help,
		get:       func(c *config.Config) string { return toggleStr(get(c)) },
		applyStep: func(c *config.Config, d int) bool { put(c, d > 0); return false },
	}
}

// newThemeColorSlider builds one R/G/B channel row of a custom-theme
// color role (text/background/accent/glow — see themeRows).
// Values are presented and edited as ordinary 0-255 sRGB bytes (see
// colors.go's srgbByte/byteToLinear) even though Config stores the
// channel as linear light — raw linear values are not a range a human
// can reason about when hand-picking a color.
func newThemeColorSlider(role, label string, ch rune, get func(*config.Config) *[3]float32) *setting {
	idx := map[rune]int{'r': 0, 'g': 1, 'b': 2}[ch]
	return &setting{
		key: "theme." + role + "." + string(ch), label: label + " " + string(ch),
		help: role + " color channel, 0-255 sRGB",
		get:  func(c *config.Config) string { return fmt.Sprintf("%d", srgbByte((*get(c))[idx])) },
		applyStep: func(c *config.Config, d int) bool {
			p := get(c)
			p[idx] = byteToLinear(srgbByte(p[idx]) + 2*d)
			return false
		},
		rangeOf: func(c *config.Config) (float64, float64, float64) {
			return float64(srgbByte((*get(c))[idx])), 0, 255
		},
	}
}

// ansiColorNames labels the 16 standard ANSI palette slots (0-15) in
// their conventional order — the same order every theme's
// Colors.Palette array uses.
var ansiColorNames = [16]string{
	"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white",
	"br-black", "br-red", "br-green", "br-yellow", "br-blue", "br-magenta", "br-cyan", "br-white",
}

// paletteChoices lists the distinct colors the built-in themes use at
// palette slot idx, in config.ThemeNames() order, deduplicated by value —
// what newPaletteSlot's cycle picker steps through. No new color data to
// maintain: it's exactly what's already sitting in each theme's own
// Colors.Palette.
func paletteChoices(idx int) [][3]float32 {
	var vals [][3]float32
	seen := map[[3]float32]bool{}
	for _, name := range config.ThemeNames() {
		v := config.Theme(name).Colors.Palette[idx]
		if !seen[v] {
			seen[v] = true
			vals = append(vals, v)
		}
	}
	return vals
}

// paletteThemeMatch reports which built-in theme (if any) currently
// supplies slot idx's color unchanged — shown in the row so
// picking a palette color also reads as "this slot = nord's blue" rather
// than a bare hex code, wherever that's true. Empty means a hand-picked
// value with no exact match, shown as a hex code instead (see
// newPaletteSlot's get).
func paletteThemeMatch(idx int, cur [3]float32) string {
	for _, name := range config.ThemeNames() {
		if config.Theme(name).Colors.Palette[idx] == cur {
			return name
		}
	}
	return ""
}

// cyclePaletteColor steps slot idx's current color to the next (d>0) or
// previous (d<0) entry in paletteChoices, wrapping around — same
// not-currently-a-choice fallback as cycleName (snap to the first entry
// rather than guessing a direction from an unrelated value).
func cyclePaletteColor(idx int, cur [3]float32, d int) [3]float32 {
	vals := paletteChoices(idx)
	if len(vals) == 0 {
		return cur
	}
	at := -1
	for i, v := range vals {
		if v == cur {
			at = i
			break
		}
	}
	if at < 0 {
		at = 0
	} else {
		n := len(vals)
		at = ((at+d)%n + n) % n
	}
	return vals[at]
}

// newPaletteSlot builds one ANSI palette color's row (see
// themeRows's "ANSI palette" section): a cycle picker stepping
// through every built-in theme's own color for that slot (paletteChoices)
// — verifying/comparing the whole 16-color table this way needs no new
// color data — plus a live swatch (settingLines) so the actual color is
// visible on the same row instead of only as a hex code. Picking a value
// that isn't the active theme's own original one flips Theme to "custom"
// (asThemeColor, same convention as the text/bg/accent/glow sliders).
func newPaletteSlot(idx int) *setting {
	return &setting{
		key:   fmt.Sprintf("colors.palette.%d", idx),
		label: fmt.Sprintf("%-2d %s", idx, ansiColorNames[idx]),
		help:  fmt.Sprintf("ANSI color %d — cycles through every built-in theme's own color for this slot", idx),
		get: func(c *config.Config) string {
			cur := c.Colors.Palette[idx]
			if name := paletteThemeMatch(idx, cur); name != "" {
				return name
			}
			return hex3(cur)
		},
		applyStep: func(c *config.Config, d int) bool {
			c.Colors.Palette[idx] = cyclePaletteColor(idx, c.Colors.Palette[idx], d)
			return false
		},
		swatch: func(c *config.Config) [3]float32 { return c.Colors.Palette[idx] },
	}
}

// toggleStr renders a boolean as a small filled/hollow-circle switch
// instead of plain "on"/"off" text.
func toggleStr(on bool) string {
	if on {
		return "● on"
	}
	return "○ off"
}

func trimFloat(v float64, dec int) string {
	s := fmt.Sprintf("%.*f", dec, v)
	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}

func roundFloat(v float64, dec int) float64 {
	p := 1
	for range dec {
		p *= 10
	}
	f := float64(p)
	return float64(int(v*f+0.5)) / f
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
