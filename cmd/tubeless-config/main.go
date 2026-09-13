// Command tubeless-config is the in-terminal settings UI for tubeless. Run
// it as the terminal's app so it draws inside the SAME window:
//
//	tubeless --shell=$(which tubeless-config)
//
// It is a plain VT text-mode program (the same family as cmd/tektest), so
// everything on screen is real terminal content: the host's renderer paints
// box/border/block glyphs through its shape-blur layer and text stays sharp
// — the config screen doubles as the renderer's test bench. Selections and
// block bars use reverse video (bright phosphor block, black text) so they
// read clearly.
//
// The screen is two tabs (Tab/Shift+Tab, or Shift+Tab's ESC[Z, to switch):
// PRESETS holds every non-color, non-font visual effect (see
// pkg/config's Effects), cycled through a list of period-accurate 80s
// monitors alongside the "modern" default (CRT emulation off); FONTS &
// THEME holds the font and every color setting (see pkg/config's ThemeColors),
// including a per-channel custom-color editor, with a live swatch/sample
// preview at the bottom. Editing any setting on either tab by hand flips
// that tab's own preset/theme name to "custom" — see adjust().
//
// Edits only take effect in memory as you navigate — nothing is written to
// ~/.config/tubeless/config.toml until you press 's'. The running tubeless
// host watches that file and re-applies non-font settings live once saved,
// rebuilding its font atlas when font/atlas fields change.
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"slices"
	"sort"
	"strings"
	"syscall"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/moozd/tubeless/pkg/config"
	"github.com/moozd/tubeless/pkg/font"
	"github.com/moozd/tubeless/pkg/platform"
)

type ui struct {
	cfg  config.Config
	path string
	cols int
	rows int

	// tab is which of the two top-level tabs is active (0 = presets, 1 =
	// fonts & theme) — see listFor/switchTab. Each tab keeps its own
	// navigation position in selByTab so switching away and back doesn't
	// lose your place.
	tab         int
	listPresets []panelRow
	listFonts   []panelRow
	list        []panelRow
	sel         int
	selByTab    [2]int

	status  string
	esc     []byte
	unsaved bool

	// Font family picker modal (see openFontPicker) — a searchable list of
	// installed families, opened from the font.family setting instead of
	// the old free-text path editor.
	fontPicker  bool
	fontQuery   string
	fontAll     []string // every installed family, fetched once and cached
	fontErr     string   // set when fc-list failed; picker falls back to free text
	fontMatches []string
	fontSel     int

	// Profile save/load modals (see openProfileSave/openProfileLoad) —
	// named full-Config snapshots under ~/.config/tubeless/profiles/.
	profileSaving  bool
	profileNameBuf string
	profileLoading bool
	profileNames   []string
	profileErr     string
	profileSel     int

	// activeProfile is the profile save() keeps in sync once one exists
	// for the current custom edits — set by loading a profile (o) or by
	// naming one the first time save() needs to create one (see save()'s
	// own doc comment). Empty means "no profile associated yet".
	activeProfile string
}

// modalOpen reports whether any full-screen modal is currently
// capturing input — the font picker or either profile dialog — so the
// normal settings-list key bindings (and CSI-sequence dispatch) know to
// step aside.
func (u *ui) modalOpen() bool {
	return u.fontPicker || u.profileSaving || u.profileLoading
}

type rowKind uint8

const (
	rowSection rowKind = iota
	rowSetting
)

type panelRow struct {
	kind    rowKind
	section string
	set     *setting
}

// setting is one adjustable row. rangeOf, when set, draws a meter bar in
// the detail pane below the list — its own (value, min, max), not shared
// global state, so a family-picker text field or a preset/theme cycler
// (choices instead) can simply leave it nil. effect/themeColor mark which
// axis (see pkg/config's Config doc comment) hand-editing this row should
// flip to "custom" — see adjust().
type setting struct {
	key        string
	label      string
	help       string
	get        func(*config.Config) string
	applyStep  func(*config.Config, int) (needsFont bool)
	rangeOf    func(*config.Config) (v, min, max float64)
	choices    func() []string
	fontFamily bool
	effect     bool
	themeColor bool
}

// version is baked in at build time via -ldflags "-X main.version=..."
// (see the Makefile's LDFLAGS) — "dev" for a plain `go build` outside it.
var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-version") {
		fmt.Println("tubeless-config " + version)
		return
	}

	// Fixes fc-list lookups (see pkg/font.SystemFamilies) when this binary
	// runs standalone rather than exec'd by tubeless — a macOS GUI-launched
	// process's PATH is missing whatever a login shell's profile adds.
	platform.FixEnv()

	path, err := config.DefaultPath()
	if err != nil {
		log.Fatalf("config dir: %v", err)
	}
	cfg, err := config.Load(path, "")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	cols, rows, err := termSize()
	if err != nil {
		cols, rows = 90, 30
	}

	raw, err := rawMode(os.Stdin.Fd())
	if err != nil {
		log.Fatalf("raw mode: %v", err)
	}
	defer raw.restore()

	u := &ui{cfg: cfg, path: path, cols: cols, rows: rows, activeProfile: cfg.ActiveProfile}
	u.buildLists()

	// Alternate screen + hidden cursor so the UI owns the window and
	// restores the shell on exit.
	fmt.Fprint(os.Stdout, "\x1b[?1049h\x1b[?25l\x1b[2J")
	defer fmt.Fprint(os.Stdout, "\x1b[?25h\x1b[?1049l")

	sigWin := make(chan os.Signal, 1)
	signal.Notify(sigWin, syscall.SIGWINCH)
	defer signal.Stop(sigWin)

	keys := make(chan byte, 64)
	go readKeys(os.Stdin, keys)

	// The loop below only redraws in response to a keypress or resize —
	// draw the initial screen once up front, or it stays blank (just the
	// 2J clear from above) until the first such event.
	u.redraw()

	for {
		select {
		case b := <-keys:
			if u.feed(b) {
				fmt.Fprint(os.Stdout, "\x1b[?25h\x1b[?1049l")
				return
			}
		case <-sigWin:
			if c, r, err := termSize(); err == nil {
				u.cols, u.rows = c, r
			}
		}
		u.redraw()
	}
}

// ---------------- input ----------------

func (u *ui) feed(b byte) (quit bool) {
	if len(u.esc) > 0 {
		// The '[' CSI introducer (0x5B) numerically falls in the "final
		// byte" range (0x40-0x7E) tested below but structurally isn't
		// one — it's always the second byte of a CSI sequence, never its
		// end. Testing for it too early there would treat every arrow key
		// (ESC [ A/B/C/D) as a 2-byte sequence that dispatches nothing,
		// discarding the buffer before the real final byte ever arrives.
		// A non-'[' second byte instead means the Escape was pressed on
		// its own — dispatch that as "cancel" and reprocess b normally.
		if len(u.esc) == 1 && b != '[' {
			u.esc = nil
			u.closeModal("cancelled")
			return u.feed(b)
		}
		u.esc = append(u.esc, b)
		if len(u.esc) >= 3 && b >= 0x40 {
			s := u.esc
			u.esc = nil
			if len(s) == 3 && s[0] == 0x1b && s[1] == '[' {
				switch s[2] {
				case 'A':
					switch {
					case u.fontPicker:
						u.moveFontSel(-1)
					case u.profileLoading:
						u.moveProfileSel(-1)
					default:
						u.move(-1)
					}
				case 'B':
					switch {
					case u.fontPicker:
						u.moveFontSel(1)
					case u.profileLoading:
						u.moveProfileSel(1)
					default:
						u.move(1)
					}
				case 'C':
					if !u.modalOpen() {
						u.adjust(1)
					}
				case 'D':
					if !u.modalOpen() {
						u.adjust(-1)
					}
				case 'H':
					if !u.modalOpen() {
						u.sel = 0
						u.firstSetting()
					}
				case 'F':
					if !u.modalOpen() {
						u.lastSetting()
					}
				case 'Z': // Shift+Tab
					if !u.modalOpen() {
						u.switchTab(-1)
					}
				}
			}
		}
		return false
	}
	if b == 0x1b {
		u.esc = append(u.esc, b)
		return false
	}
	if u.fontPicker {
		switch b {
		case '\r', '\n':
			u.commitFontPicker()
		case 0x7f, '\b':
			if rs := []rune(u.fontQuery); len(rs) > 0 {
				u.fontQuery = string(rs[:len(rs)-1])
			}
			u.refreshFontMatches()
		default:
			if b >= 0x20 {
				u.fontQuery += string(rune(b))
				u.refreshFontMatches()
			}
		}
		return false
	}
	if u.profileSaving {
		switch b {
		case '\r', '\n':
			u.commitProfileSave()
		case 0x7f, '\b':
			if rs := []rune(u.profileNameBuf); len(rs) > 0 {
				u.profileNameBuf = string(rs[:len(rs)-1])
			}
		default:
			if b >= 0x20 {
				u.profileNameBuf += string(rune(b))
			}
		}
		return false
	}
	if u.profileLoading {
		if b == '\r' || b == '\n' {
			u.commitProfileLoad()
		}
		return false
	}
	switch b {
	case 'q':
		return true
	case 's':
		u.save()
	case 'r':
		u.resetPreset()
	case 'p':
		u.openProfileSave()
	case 'o':
		u.openProfileLoad()
	case '\t':
		u.switchTab(1)
	case '\r', '\n':
		s := u.cur()
		if s != nil && s.fontFamily {
			u.openFontPicker()
		}
	case 'k':
		u.move(-1)
	case 'j':
		u.move(1)
	case 'l':
		u.adjust(1)
	case 'h':
		u.adjust(-1)
	}
	return false
}

// listFor returns the row list for a tab index — the single source both
// buildLists and switchTab read from, so they can't drift. Fonts &
// theme is tab 0 (the first thing you see on open); presets is tab 1.
func (u *ui) listFor(tab int) []panelRow {
	if tab == 1 {
		return u.listPresets
	}
	return u.listFonts
}

// switchTab moves to tab (0 or 1, wrapping) after remembering the
// outgoing tab's cursor position, then restores the incoming tab's own —
// snapping onto the first real setting if that position landed on a
// section header (always true the first time a tab is visited, since
// selByTab starts zeroed).
func (u *ui) switchTab(d int) {
	u.selByTab[u.tab] = u.sel
	u.tab = ((u.tab+d)%2 + 2) % 2
	u.list = u.listFor(u.tab)
	u.sel = u.selByTab[u.tab]
	if u.sel >= len(u.list) || u.list[u.sel].kind != rowSetting {
		u.firstSetting()
	}
}

func (u *ui) firstSetting() {
	for i, r := range u.list {
		if r.kind == rowSetting {
			u.sel = i
			return
		}
	}
}

func (u *ui) lastSetting() {
	for i := len(u.list) - 1; i >= 0; i-- {
		if u.list[i].kind == rowSetting {
			u.sel = i
			return
		}
	}
}

func (u *ui) move(d int) {
	n := len(u.list)
	for i := 0; i < n; i++ {
		u.sel = (u.sel + d + n) % n
		if u.list[u.sel].kind == rowSetting {
			return
		}
	}
}

func (u *ui) cur() *setting {
	if u.sel < len(u.list) && u.list[u.sel].kind == rowSetting {
		return u.list[u.sel].set
	}
	return nil
}

// adjust applies one step to the selected setting, then — the mechanism
// behind "editing a preset/theme's values makes it custom" — flips that
// axis's name to "custom" if the setting belongs to it. The preset/theme
// cycle settings themselves don't set effect/themeColor, since cycling
// them already sets a real name via applyStep.
func (u *ui) adjust(d int) {
	s := u.cur()
	if s == nil {
		return
	}
	if needsFont := s.applyStep(&u.cfg, d); needsFont {
		u.status = "font/atlas changed — press s to save"
	}
	if s.effect {
		u.cfg.Preset = "custom"
	}
	if s.themeColor {
		u.cfg.Theme = "custom"
	}
	u.dirty()
}

// openFontPicker opens the font.family search modal, fetching the system's
// installed families (via fontconfig) once and caching them for the rest
// of the session — fc-list is a subprocess call, not worth repeating on
// every open. If it fails (fontconfig not installed), the picker falls
// back to a plain search box the user can still type an exact family
// name into.
func (u *ui) openFontPicker() {
	if u.fontAll == nil && u.fontErr == "" {
		names, err := font.SystemFamilies()
		if err != nil {
			u.fontErr = err.Error()
		} else {
			u.fontAll = names
		}
	}
	u.fontPicker = true
	u.fontQuery = u.cfg.Font.Family
	u.fontSel = 0
	u.refreshFontMatches()
}

func (u *ui) closeFontPicker(status string) {
	u.fontPicker = false
	u.status = status
}

// commitFontPicker sets Font.Family to the highlighted match, or — if
// fc-list found nothing matching (or isn't available at all) — to
// whatever was typed, so an exact family name always works even without
// fontconfig enumeration.
func (u *ui) commitFontPicker() {
	family := strings.TrimSpace(u.fontQuery)
	if u.fontSel < len(u.fontMatches) {
		family = u.fontMatches[u.fontSel]
	}
	u.cfg.Font.Family = family
	u.closeFontPicker("font family changed — press s to save")
	u.dirty()
}

func (u *ui) moveFontSel(d int) {
	n := len(u.fontMatches)
	if n == 0 {
		return
	}
	u.fontSel = clampInt(u.fontSel+d, 0, n-1)
}

// closeModal cancels whichever modal (see modalOpen) is currently open —
// the Escape handler doesn't know or care which one, so it just closes
// them all; at most one is ever open at a time.
func (u *ui) closeModal(status string) {
	u.fontPicker = false
	u.profileSaving = false
	u.profileLoading = false
	u.status = status
}

// setActiveProfile records name as the profile this session is now
// associated with, in both places that matters: u.activeProfile (what
// the header shows and save() checks live) and u.cfg.ActiveProfile
// (what actually persists the association into config.toml/the profile
// file itself across separate runs of this program — see Config's own
// doc comment on the field).
func (u *ui) setActiveProfile(name string) {
	u.activeProfile = name
	u.cfg.ActiveProfile = name
}

// openProfileSave opens the "save profile" name-entry modal — see
// pkg/config's SaveProfile for where it ends up.
func (u *ui) openProfileSave() {
	u.profileSaving = true
	u.profileNameBuf = ""
}

// commitProfileSave writes the current in-memory config — including any
// unsaved edits, same as everything else in this UI — as a named
// snapshot, independent of whether it's ever been written to the live
// config.toml via 's'.
func (u *ui) commitProfileSave() {
	name := strings.TrimSpace(u.profileNameBuf)
	u.profileSaving = false
	if name == "" {
		u.status = "profile save cancelled — empty name"
		return
	}
	u.setActiveProfile(name)
	if err := config.SaveProfile(name, u.cfg); err != nil {
		u.status = "profile save failed: " + err.Error()
		return
	}
	u.status = "saved profile → " + name
}

// openProfileLoad opens the "load profile" list modal, listing every
// profile SaveProfile has ever written.
func (u *ui) openProfileLoad() {
	names, err := config.ProfileNames()
	if err != nil {
		u.profileErr = err.Error()
	} else {
		u.profileErr = ""
	}
	u.profileNames = names
	u.profileSel = 0
	u.profileLoading = true
}

func (u *ui) moveProfileSel(d int) {
	n := len(u.profileNames)
	if n == 0 {
		return
	}
	u.profileSel = clampInt(u.profileSel+d, 0, n-1)
}

// commitProfileLoad replaces u.cfg wholesale with the selected profile —
// like every other edit in this UI, this only takes effect in memory
// until 's' writes it to the live config.toml.
func (u *ui) commitProfileLoad() {
	u.profileLoading = false
	if u.profileSel >= len(u.profileNames) {
		u.status = "no profile selected"
		return
	}
	name := u.profileNames[u.profileSel]
	cfg, err := config.LoadProfile(name)
	if err != nil {
		u.status = "profile load failed: " + err.Error()
		return
	}
	u.cfg = cfg
	u.setActiveProfile(name)
	u.status = "loaded profile → " + name
	u.dirty()
}

// refreshFontMatches re-filters fontAll by fontQuery (fuzzyMatch) and
// sorts by match quality, tightest first.
func (u *ui) refreshFontMatches() {
	u.fontMatches = u.fontMatches[:0]
	scores := map[string]int{}
	for _, name := range u.fontAll {
		if score, ok := fuzzyMatch(u.fontQuery, name); ok {
			u.fontMatches = append(u.fontMatches, name)
			scores[name] = score
		}
	}
	sort.SliceStable(u.fontMatches, func(i, j int) bool {
		return scores[u.fontMatches[i]] < scores[u.fontMatches[j]]
	})
	if n := len(u.fontMatches); u.fontSel >= n {
		u.fontSel = max(0, n-1)
	}
}

// fuzzyMatch is a compact fzf-style subsequence filter: query matches
// candidate if every query rune appears in candidate, in order,
// case-insensitively. The score (lower is a tighter match) rewards a
// match starting earlier in the name and spanning fewer of its runes.
func fuzzyMatch(query, candidate string) (score int, ok bool) {
	if query == "" {
		return 0, true
	}
	q := []rune(strings.ToLower(query))
	c := []rune(strings.ToLower(candidate))
	qi, start := 0, -1
	for ci := range c {
		if qi < len(q) && c[ci] == q[qi] {
			if start < 0 {
				start = ci
			}
			qi++
		}
	}
	if qi < len(q) {
		return 0, false
	}
	span := len(c) - start
	return start*100 + span, true
}

// save writes u.cfg to the live config.toml, unconditionally — this is
// what actually takes edits live (see dirty()'s doc comment). It also
// keeps a profile in sync:
//   - a profile currently loaded (activeProfile — see the header, which
//     always shows it while one's active) gets every save's changes,
//     full stop, not just ones that happen to touch a "custom"-marking
//     setting — loading a profile and tweaking anything at all, font
//     included, means that profile is what you're editing now.
//   - with no profile loaded, a save is just a save — until the edit is
//     one that flips Preset or Theme to "custom" (see adjust()), at
//     which point this opens the same name-entry modal 'p' does, so a
//     hand-tuned combination is never left with no name to find it by
//     later.
func (u *ui) save() {
	if err := config.Save(u.path, u.cfg); err != nil {
		u.status = "save failed: " + err.Error()
		return
	}
	u.unsaved = false
	switch {
	case u.activeProfile != "":
		if err := config.SaveProfile(u.activeProfile, u.cfg); err != nil {
			u.status = "saved, but profile update failed: " + err.Error()
			return
		}
		u.status = "saved → " + u.path + " · profile \"" + u.activeProfile + "\" updated"
	case u.cfg.Preset == "custom" || u.cfg.Theme == "custom":
		u.openProfileSave()
		u.status = "saved → " + u.path + " — name a profile to keep these custom changes"
	default:
		u.status = "saved → " + u.path
	}
}

// resetPreset snaps the active tab's own axis back to its named
// preset/theme's canonical values — modern/rosepine if that axis is
// currently "custom" (nothing named to snap back to).
func (u *ui) resetPreset() {
	if u.tab == 1 {
		name := u.cfg.Preset
		if name == "" || name == "custom" {
			name = "modern"
		}
		applyEffectsPresetCfg(&u.cfg, name)
		u.status = "reset to " + name + " preset"
	} else {
		name := u.cfg.Theme
		if name == "" || name == "custom" {
			name = "rosepine"
		}
		applyThemeCfg(&u.cfg, name)
		u.status = "reset to " + name + " theme"
	}
	u.dirty()
}

// dirty marks u.cfg as having an unsaved edit. It deliberately does not
// write to disk — the host terminal only re-applies config.toml on its own
// watch cycle, so writing here would mean every arrow-key nudge takes
// effect immediately in the running terminal; changes only take effect
// once the user explicitly presses 's' (see save()).
func (u *ui) dirty() {
	u.unsaved = true
}

// cycleName steps current by d through names, wrapping — the shared step
// behind both the preset and theme cycle settings.
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

// applyEffectsPresetCfg seeds c's effects axis from a named preset and —
// the behavior behind "selecting a monitor preset switches its theme in
// too" — also applies that preset's linked theme, when MonitorTheme has
// one.
func applyEffectsPresetCfg(c *config.Config, name string) {
	e := config.EffectsPreset(name)
	c.Preset = name
	c.Blur, c.Rounding, c.Cursor = e.Blur, e.Rounding, e.Cursor
	c.Face, c.Contrast, c.CRT = e.Face, e.Contrast, e.CRT
	if theme, ok := config.MonitorTheme(name); ok {
		applyThemeCfg(c, theme)
	}
}

// applyThemeCfg seeds c's color axis from a named theme.
func applyThemeCfg(c *config.Config, name string) {
	t := config.Theme(name)
	c.Theme = name
	c.TrueColor, c.Phosphor, c.Colors = t.TrueColor, t.Phosphor, t.Colors
}

// ---------------- settings model ----------------

func (u *ui) buildLists() {
	u.listPresets = u.buildPresetsList()
	u.listFonts = u.buildFontsThemeList()
	u.tab = 0
	u.list = u.listFonts
	u.firstSetting()
}

// asEffect/asThemeColor mark a setting as belonging to the preset/theme
// axis (see adjust()) and return it, so a row can be built and marked in
// one add(asEffect(newSlider(...))) call.
func asEffect(s *setting) *setting     { s.effect = true; return s }
func asThemeColor(s *setting) *setting { s.themeColor = true; return s }

// newSlider builds a numeric setting: get/put close over one Config
// field, step adjusts it by ±step per arrow press (clamped to
// [min,max]), and rangeOf exposes the same (value, min, max) for the
// detail pane's meter bar.
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
// color role (text/background/accent/glow — see buildFontsThemeList).
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

// toggleStr renders a boolean as a small filled/hollow-circle switch
// instead of plain "on"/"off" text.
func toggleStr(on bool) string {
	if on {
		return "● on"
	}
	return "○ off"
}

// buildPresetsList is the PRESETS tab: a cycle through every registered
// effects preset (see pkg/config's EffectsPresetNames), followed by
// every individual effect it seeds — editing any of them by hand flips
// Preset to "custom" (see adjust()).
func (u *ui) buildPresetsList() []panelRow {
	var list []panelRow
	section := func(name string) { list = append(list, panelRow{kind: rowSection, section: name}) }
	add := func(s *setting) { list = append(list, panelRow{kind: rowSetting, set: s}) }

	section("preset")
	add(&setting{
		key: "preset", label: "monitor / look",
		help:    "cycles every effects preset — a monitor's own theme switches in with it, where one exists",
		choices: func() []string { return config.EffectsPresetNames() },
		get:     func(c *config.Config) string { return c.Preset },
		applyStep: func(c *config.Config, d int) bool {
			applyEffectsPresetCfg(c, cycleName(config.EffectsPresetNames(), c.Preset, d))
			return false
		},
	})

	section("blur — boxes & borders")
	add(asEffect(newSlider("blur.radius", "radius", "gaussian spread in px on blocks & box glyphs", "px", 1, 0.1, 0, 8,
		func(c *config.Config) float64 { return float64(c.Blur.Radius) },
		func(c *config.Config, v float64) { c.Blur.Radius = float32(v) })))
	add(asEffect(newSlider("blur.strength", "strength", "how strongly the blur replaces the sharp shapes", "", 2, 0.01, 0, 1,
		func(c *config.Config) float64 { return float64(c.Blur.Strength) },
		func(c *config.Config, v float64) { c.Blur.Strength = float32(v) })))

	section("rounding — solid blocks & backgrounds")
	add(asEffect(newSlider("rounding.radius", "corner radius", "true geometric corner radius on background fills & solid block glyphs (█▀▄▌▐ etc.)", "px", 1, 0.1, 0, 8,
		func(c *config.Config) float64 { return float64(c.Rounding.Radius) },
		func(c *config.Config, v float64) { c.Rounding.Radius = float32(v) })))

	section("face — tube")
	add(asEffect(newSlider("face.bg_tint", "bg tint", "brightness of the unlit screen", "", 3, 0.005, 0, 0.5,
		func(c *config.Config) float64 { return float64(c.Face.BgTint) },
		func(c *config.Config, v float64) { c.Face.BgTint = float32(v) })))
	add(asEffect(newSlider("face.inset_shadow", "inset shadow", "radial falloff to the corners", "", 2, 0.01, 0, 1,
		func(c *config.Config) float64 { return float64(c.Face.InsetShadow) },
		func(c *config.Config, v float64) { c.Face.InsetShadow = float32(v) })))

	section("cursor")
	add(asEffect(newSlider("cursor.glow", "glow", "halo width of the block cursor", "px", 1, 0.1, 0, 8,
		func(c *config.Config) float64 { return float64(c.Cursor.Glow) },
		func(c *config.Config, v float64) { c.Cursor.Glow = float32(v) })))
	add(asEffect(newSlider("cursor.pulse_period", "pulse period", "breathing period of the cursor", "s", 2, 0.05, 0.2, 4,
		func(c *config.Config) float64 { return float64(c.Cursor.PulsePeriod) },
		func(c *config.Config, v float64) { c.Cursor.PulsePeriod = float32(v) })))

	section("contrast")
	add(asEffect(newSlider("contrast.min_delta", "min contrast", "minimum fg/bg gap on the mono ramp", "", 2, 0.01, 0, 1,
		func(c *config.Config) float64 { return float64(c.Contrast.MinDelta) },
		func(c *config.Config, v float64) { c.Contrast.MinDelta = float32(v) })))

	// Every CRT effect below is off by default (0) in the modern
	// preset and independently configurable — see pkg/config/crt.go's
	// doc comment for why these are plain floats rather than a separate
	// Enabled flag.
	section("crt — curvature")
	add(asEffect(newSlider("crt.curvature.amount", "amount", "barrel-distortion strength; 0 = flat", "", 2, 0.01, 0, 0.5,
		func(c *config.Config) float64 { return float64(c.CRT.Curvature.Amount) },
		func(c *config.Config, v float64) { c.CRT.Curvature.Amount = float32(v) })))

	section("crt — scanlines")
	add(asEffect(newSlider("crt.scanlines.intensity", "intensity", "darkens alternating lines; 0 = off", "", 2, 0.01, 0, 1,
		func(c *config.Config) float64 { return float64(c.CRT.Scanlines.Intensity) },
		func(c *config.Config, v float64) { c.CRT.Scanlines.Intensity = float32(v) })))
	add(asEffect(newSlider("crt.scanlines.period", "period", "device px per line-pair", "px", 1, 0.5, 1, 12,
		func(c *config.Config) float64 { return float64(c.CRT.Scanlines.Period) },
		func(c *config.Config, v float64) { c.CRT.Scanlines.Period = float32(v) })))

	section("crt — chromatic aberration")
	add(asEffect(newSlider("crt.aberration.amount", "amount", "red/blue channel offset; 0 = off", "", 4, 0.0005, 0, 0.02,
		func(c *config.Config) float64 { return float64(c.CRT.Aberration.Amount) },
		func(c *config.Config, v float64) { c.CRT.Aberration.Amount = float32(v) })))

	section("crt — shadow mask")
	add(asEffect(newSlider("crt.shadow_mask.intensity", "intensity", "RGB triad overlay strength; 0 = off", "", 2, 0.01, 0, 1,
		func(c *config.Config) float64 { return float64(c.CRT.ShadowMask.Intensity) },
		func(c *config.Config, v float64) { c.CRT.ShadowMask.Intensity = float32(v) })))
	add(asEffect(newSlider("crt.shadow_mask.cell_size", "cell size", "device px per triad column", "px", 1, 0.5, 1, 12,
		func(c *config.Config) float64 { return float64(c.CRT.ShadowMask.CellSize) },
		func(c *config.Config, v float64) { c.CRT.ShadowMask.CellSize = float32(v) })))

	section("crt — noise")
	add(asEffect(newSlider("crt.noise.intensity", "intensity", "analog signal noise; 0 = off", "", 3, 0.005, 0, 0.2,
		func(c *config.Config) float64 { return float64(c.CRT.Noise.Intensity) },
		func(c *config.Config, v float64) { c.CRT.Noise.Intensity = float32(v) })))

	section("crt — flicker")
	add(asEffect(newSlider("crt.flicker.amount", "amount", "whole-screen brightness jitter; 0 = off", "", 2, 0.01, 0, 1,
		func(c *config.Config) float64 { return float64(c.CRT.Flicker.Amount) },
		func(c *config.Config, v float64) { c.CRT.Flicker.Amount = float32(v) })))
	add(asEffect(newSlider("crt.flicker.speed", "speed", "flicker rate", "hz", 1, 0.5, 0.5, 30,
		func(c *config.Config) float64 { return float64(c.CRT.Flicker.Speed) },
		func(c *config.Config, v float64) { c.CRT.Flicker.Speed = float32(v) })))

	section("crt — phosphor decay")
	add(asEffect(newSlider("crt.phosphor_decay.decay_seconds", "decay seconds", "afterglow trail length; 0 = off", "s", 2, 0.05, 0, 3,
		func(c *config.Config) float64 { return float64(c.CRT.PhosphorDecay.DecaySeconds) },
		func(c *config.Config, v float64) { c.CRT.PhosphorDecay.DecaySeconds = float32(v) })))

	section("crt — aspect ratio")
	add(asEffect(newSlider("crt.aspect_ratio.width", "width", "letterbox/pillarbox to width:height instead of filling the window; 0 (either field) = off, full window", "", 0, 1, 0, 1200,
		func(c *config.Config) float64 { return float64(c.CRT.AspectRatio.Width) },
		func(c *config.Config, v float64) { c.CRT.AspectRatio.Width = float32(v) })))
	add(asEffect(newSlider("crt.aspect_ratio.height", "height", "paired with width above; 0 (either field) = off, full window", "", 0, 1, 0, 1200,
		func(c *config.Config) float64 { return float64(c.CRT.AspectRatio.Height) },
		func(c *config.Config, v float64) { c.CRT.AspectRatio.Height = float32(v) })))

	return list
}

// buildFontsThemeList is the FONTS & THEME tab: font shaping settings,
// a cycle through every registered color theme, and a per-channel
// custom-color editor (text/background/accent/glow) — editing any color
// channel by hand flips Theme to "custom" (see adjust()). See
// drawColorPreview for the live swatch/sample preview this feeds.
func (u *ui) buildFontsThemeList() []panelRow {
	var list []panelRow
	section := func(name string) { list = append(list, panelRow{kind: rowSection, section: name}) }
	add := func(s *setting) { list = append(list, panelRow{kind: rowSetting, set: s}) }

	section("font")
	add(&setting{
		key: "font.size", label: "size", help: "logical pixel height (rebuilds the atlas)",
		get: func(c *config.Config) string { return fmt.Sprintf("%d px", c.Font.Size) },
		applyStep: func(c *config.Config, d int) bool {
			c.Font.Size = clampInt(c.Font.Size+2*d, 10, 96)
			return true
		},
		rangeOf: func(c *config.Config) (float64, float64, float64) { return float64(c.Font.Size), 10, 96 },
	})
	add(&setting{
		key: "atlas.scale", label: "atlas scale", help: "glyph supersampling (rebuilds the atlas)",
		get: func(c *config.Config) string { return fmt.Sprintf("%d×", c.Atlas.Scale) },
		applyStep: func(c *config.Config, d int) bool {
			c.Atlas.Scale = clampInt(c.Atlas.Scale+d, 1, 8)
			return true
		},
		rangeOf: func(c *config.Config) (float64, float64, float64) { return float64(c.Atlas.Scale), 1, 8 },
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
		key: "font.family", label: "family", help: "installed font family; Enter to search", fontFamily: true,
		get: func(c *config.Config) string {
			if c.Font.Family == "" {
				return "FiraCode Nerd Font"
			}
			return c.Font.Family
		},
		// ←/→ have nothing to step through here — Enter opens the search
		// picker instead (see feed()) — but adjust() always calls
		// applyStep unconditionally, so this still needs a no-op rather
		// than staying nil.
		applyStep: func(c *config.Config, d int) bool { return false },
	})

	section("theme")
	add(&setting{
		key: "theme", label: "theme",
		help:    "cycles every color theme",
		choices: func() []string { return config.ThemeNames() },
		get:     func(c *config.Config) string { return c.Theme },
		applyStep: func(c *config.Config, d int) bool {
			applyThemeCfg(c, cycleName(config.ThemeNames(), c.Theme, d))
			return false
		},
	})

	section("custom theme colors")
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

	return list
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

// SGR helpers.
const (
	sgrReset   = "\x1b[0m"
	sgrBold    = "\x1b[1m"
	sgrDim     = "\x1b[2m"
	sgrReverse = "\x1b[7m"
)

// The UI is plain terminal content: box-drawing borders, block meters,
// reverse-video selection rows and background-colored regions. The host
// paints all of it through the same pipeline as the shell, so borders and
// blocks come from its blurred shape layer while text stays sharp — the
// config screen doubles as a live test bench for those effects.

// accent is the one color that ties every pane together: the theme's own
// CRT-chrome accent (Phosphor.High — see pkg/config's doc comment on
// Phosphor), set for every theme, monochrome or TrueColor alike, so pane
// headers/borders always paint in a color that actually belongs to the
// active theme instead of a fixed one.
func (u *ui) accent() [3]float32 { return u.cfg.Phosphor.High }

// tabTitle names a tab index for both the tab bar and the settings box's
// own title.
func tabTitle(tab int) string {
	if tab == 1 {
		return "presets"
	}
	return "fonts & theme"
}

func (u *ui) redraw() {
	b := &strings.Builder{}
	b.WriteString("\x1b[2J")

	if u.cols < 90 || u.rows < 22 {
		u.revRow(b, 1, " window too small — resize larger (needs 90x22+) ")
		flush(b)
		return
	}
	cols, rows := u.cols, u.rows
	accent := u.accent()

	head := " TUBELESS CONFIG"
	profileLabel := u.activeProfile
	if profileLabel == "" {
		profileLabel = "none — p to save one, o to load one"
	}
	head += "  ·  profile: " + profileLabel
	if u.unsaved {
		head += "  ·  unsaved changes (s to save)"
	}
	if u.fontPicker {
		head += "  ·  searching fonts…"
	}
	u.at(b, 1, 1, truecolorBg(accent)+contrastFg(accent)+sgrBold+colPad(trunc(head, cols), cols)+sgrReset)
	u.drawTabs(b, 2, accent)

	// One bordered pane (the settings list) plus two unboxed strips below
	// it (detail, and — fonts & theme only — a color/sample preview) is
	// the whole layout: far fewer borders on screen at once than a
	// separate box per pane.
	detailH := 3
	previewH := 0
	if u.tab == 0 && rows >= 26 {
		previewH = 6
	}
	listTop := 3
	listBottom := max(listTop+2, rows-1-detailH-previewH)

	u.drawSettings(b, 1, listTop, cols, listBottom, accent)
	u.drawDetail(b, listBottom+1, cols)
	if previewH > 0 {
		u.drawColorPreview(b, listBottom+1+detailH, cols, accent)
	}
	u.footerRow(b, rows, " ⇥ switch tab · ↑↓/jk select · ←→/hl adjust · s save · p profile save · o profile load · r reset · q quit ")

	switch {
	case u.fontPicker:
		u.drawFontPicker(b)
	case u.profileSaving:
		u.drawProfileSave(b)
	case u.profileLoading:
		u.drawProfileLoad(b)
	}
	flush(b)
}

// drawTabs renders the two-tab segmented control: the active tab paints
// as a solid accent bar (matching the settings list's own selected-row
// treatment), the inactive one dims — no border, just color.
func (u *ui) drawTabs(b *strings.Builder, y int, accent [3]float32) {
	var line strings.Builder
	for i := range 2 {
		chip := " " + strings.ToUpper(tabTitle(i)) + " "
		if i == u.tab {
			line.WriteString(truecolorBg(accent) + contrastFg(accent) + sgrBold + chip + sgrReset)
		} else {
			line.WriteString(truecolorFg(accent) + sgrDim + chip + sgrReset)
		}
		line.WriteString("  ")
	}
	u.at(b, y, 1, line.String())
}

// at writes content at (y, x), both 1-based — the positioned equivalent
// of row(), for panes that don't start at column 1.
func (u *ui) at(b *strings.Builder, y, x int, content string) {
	fmt.Fprintf(b, "\x1b[%d;%dH%s", y, x, content)
}

// styledLine composes one pane row's interior content while tracking its
// true on-screen column width alongside the ANSI-laden string — plain()
// measures via colLen, chunk() trusts a caller-supplied width for
// already-colored fixed-width fragments (meterStr/swatch blocks, whose
// own escape codes colLen can't see through). Building rows this way is
// what lets drawSettings/drawColorPreview embed real color inside a
// bordered pane without corrupting the border alignment math trunc/
// colPad alone would get wrong on a string that already contains escape
// sequences.
type styledLine struct {
	buf strings.Builder
	w   int
}

func (s *styledLine) plain(text string) {
	s.buf.WriteString(text)
	s.w += colLen(text)
}

func (s *styledLine) chunk(text string, width int) {
	s.buf.WriteString(text)
	s.w += width
}

// paneTitle draws a rounded-corner top border with the title set inline,
// in the accent color — every pane's outline header. A thin accent-tinted
// outline reads calmer than a solid color-filled bar, closer to Claude
// Code's own bordered-box chrome, while still tying every pane to the
// active theme's own color the way a solid bar would.
func (u *ui) paneTitle(b *strings.Builder, y, x0, w int, title string, accent [3]float32) {
	u.at(b, y, x0, truecolorFg(accent)+sgrBold+boxTop(strings.ToUpper(title), w)+sgrReset)
}

// paneBottom draws a pane's accent-tinted rounded bottom border.
func (u *ui) paneBottom(b *strings.Builder, y, x0, w int, accent [3]float32) {
	u.at(b, y, x0, truecolorFg(accent)+boxBottom(w)+sgrReset)
}

// paneRow draws one bordered content row from plain text: content is
// colPad/trunc'd to the pane's interior width first (so it must not
// already contain ANSI codes — see paneRowRaw for that case), then
// optionally wrapped in style (a dim/bold/color SGR prefix) or, when
// selected, painted as a solid accent bar (the settings list's
// highlighted row).
func (u *ui) paneRow(b *strings.Builder, y, x0, w int, content, style string, accent [3]float32, selected bool) {
	innerW := w - 2
	line := colPad(trunc(content, innerW), innerW)
	switch {
	case selected:
		line = truecolorBg(accent) + contrastFg(accent) + sgrBold + line + sgrReset
	case style != "":
		line = style + line + sgrReset
	}
	edge := truecolorFg(accent)
	u.at(b, y, x0, edge+"│"+sgrReset+line+edge+"│"+sgrReset)
}

// drawFontPicker overlays a centered modal on top of the normal screen (it
// draws last, after everything else in redraw): a search box, then either
// the matching family list or — when fc-list wasn't available — a hint
// that Enter uses whatever was typed literally. Tinted with the theme's
// accent, matching every other pane, for visual consistency.
func (u *ui) drawFontPicker(b *strings.Builder) {
	cols, rows := u.cols, u.rows
	w := min(cols-4, 60)
	h := min(rows-4, 20)
	if w < 20 || h < 6 {
		return
	}
	x0 := (cols - w) / 2
	y0 := (rows - h) / 2
	accent := u.accent()

	line := func(y int, s string) {
		fmt.Fprintf(b, "\x1b[%d;%dH%s%s%s", y, x0+1, truecolorFg(accent), s, sgrReset)
	}

	line(y0, boxTop("font family", w))
	line(y0+1, boxText("search: "+u.fontQuery+"_", w))
	line(y0+2, "│"+strings.Repeat("─", w-2)+"│")

	listY0, listY1 := y0+3, y0+h-2
	switch {
	case u.fontErr != "":
		line(listY0, boxText(trunc("fontconfig unavailable: "+u.fontErr, w-4), w))
		line(listY0+1, boxText("type an exact family name, Enter to use it", w))
		for y := listY0 + 2; y <= listY1; y++ {
			line(y, boxText("", w))
		}
	case len(u.fontMatches) == 0:
		line(listY0, boxText("no match — Enter uses \""+u.fontQuery+"\" as typed", w))
		for y := listY0 + 1; y <= listY1; y++ {
			line(y, boxText("", w))
		}
	default:
		innerW := w - 2
		capRows := listY1 - listY0 + 1
		start := 0
		if len(u.fontMatches) > capRows && u.fontSel >= capRows {
			start = u.fontSel - capRows + 1
		}
		for y := listY0; y <= listY1; y++ {
			i := start + (y - listY0)
			content := colPad(strings.Repeat(" ", innerW), innerW)
			if i < len(u.fontMatches) {
				content = colPad(" "+trunc(u.fontMatches[i], innerW-1), innerW)
			}
			if i == u.fontSel && i < len(u.fontMatches) {
				line(y, "│"+sgrReverse+content+sgrReset+"│")
			} else {
				line(y, "│"+content+"│")
			}
		}
	}
	line(y0+h-1, boxBottom(w))
	line(y0+h, trunc(" ↑↓ select · Enter pick · Esc cancel ", cols))
}

// drawProfileSave overlays a small centered name-entry modal — the
// counterpart to drawFontPicker's search box, for naming a new profile
// snapshot (see openProfileSave/commitProfileSave).
func (u *ui) drawProfileSave(b *strings.Builder) {
	cols, rows := u.cols, u.rows
	w := min(cols-4, 50)
	if w < 20 {
		return
	}
	x0, y0 := (cols-w)/2, (rows-4)/2
	accent := u.accent()
	line := func(y int, s string) {
		fmt.Fprintf(b, "\x1b[%d;%dH%s%s%s", y, x0+1, truecolorFg(accent), s, sgrReset)
	}
	line(y0, boxTop("save profile", w))
	line(y0+1, boxText("name: "+u.profileNameBuf+"_", w))
	line(y0+2, boxBottom(w))
	line(y0+3, trunc(" Enter to save · Esc cancel ", cols))
}

// drawProfileLoad overlays a centered scrollable list of every saved
// profile (see openProfileLoad/commitProfileLoad) — the same list-modal
// shape as drawFontPicker's match list, without a search box.
func (u *ui) drawProfileLoad(b *strings.Builder) {
	cols, rows := u.cols, u.rows
	w := min(cols-4, 50)
	h := min(rows-4, 16)
	if w < 20 || h < 6 {
		return
	}
	x0, y0 := (cols-w)/2, (rows-h)/2
	accent := u.accent()
	line := func(y int, s string) {
		fmt.Fprintf(b, "\x1b[%d;%dH%s%s%s", y, x0+1, truecolorFg(accent), s, sgrReset)
	}

	line(y0, boxTop("load profile", w))
	listY0, listY1 := y0+1, y0+h-2
	switch {
	case u.profileErr != "":
		line(listY0, boxText(trunc("error: "+u.profileErr, w-4), w))
		for y := listY0 + 1; y <= listY1; y++ {
			line(y, boxText("", w))
		}
	case len(u.profileNames) == 0:
		line(listY0, boxText("no saved profiles yet — p to save one", w))
		for y := listY0 + 1; y <= listY1; y++ {
			line(y, boxText("", w))
		}
	default:
		innerW := w - 2
		capRows := listY1 - listY0 + 1
		start := 0
		if len(u.profileNames) > capRows && u.profileSel >= capRows {
			start = u.profileSel - capRows + 1
		}
		for y := listY0; y <= listY1; y++ {
			i := start + (y - listY0)
			content := colPad(strings.Repeat(" ", innerW), innerW)
			if i < len(u.profileNames) {
				content = colPad(" "+trunc(u.profileNames[i], innerW-1), innerW)
			}
			if i == u.profileSel && i < len(u.profileNames) {
				line(y, "│"+sgrReverse+content+sgrReset+"│")
			} else {
				line(y, "│"+content+"│")
			}
		}
	}
	line(y0+h-1, boxBottom(w))
	line(y0+h, trunc(" ↑↓ select · Enter load · Esc cancel ", cols))
}

// drawSettings renders the active tab's section+setting list inside its
// own pane — the primary nav surface, spanning the full width. The
// selected row paints as a solid accent bar (the theme's own color, not
// a fixed reverse-video invert); section headers dim in the same accent.
// The label/value gap is plain spaces, not a dotted leader — no dots
// anywhere in this UI.
func (u *ui) drawSettings(b *strings.Builder, x0, y0, w, y1 int, accent [3]float32) {
	innerW := w - 2
	u.paneTitle(b, y0, x0, w, tabTitle(u.tab), accent)
	u.paneBottom(b, y1, x0, w, accent)

	// A blank row of breathing room under the title, rather than the list
	// starting flush against the border.
	u.paneRow(b, y0+1, x0, w, "", "", accent, false)
	innerY0, innerY1 := y0+2, y1-1
	capRows := innerY1 - innerY0 + 1
	n := len(u.list)
	start := 0
	if n > capRows && u.sel >= capRows {
		start = u.sel - capRows + 1
	}
	dimAccent := truecolorFg(accent) + sgrDim

	for y := innerY0; y <= innerY1; y++ {
		i := start + (y - innerY0)
		sel := i < n && i == u.sel
		plain, style := "", ""
		if i < n {
			r := u.list[i]
			switch r.kind {
			case rowSection:
				plain = "  " + strings.ToUpper(r.section)
				style = dimAccent
			case rowSetting:
				value := r.set.get(&u.cfg)
				vl := colLen(value)
				label := trunc(r.set.label, max(0, innerW-vl-3))
				ll := colLen(label)
				gap := innerW - 2 - ll - vl
				lead := " "
				if !sel {
					lead = "  "
				}
				plain = lead + label + strings.Repeat(" ", max(1, gap)) + value
			}
		}
		u.paneRow(b, y, x0, w, plain, style, accent, sel)
	}
}

// choiceChips renders names as a space-separated inline list with the
// active one bracketed — the "here's the whole list of monitors/themes"
// view the detail pane shows for a preset/theme cycle setting, since the
// row itself can only show the current value.
func choiceChips(names []string, active string, maxW int) string {
	var b strings.Builder
	for _, n := range names {
		chip := n
		if n == active {
			chip = "[" + n + "]"
		}
		sep := "  "
		if b.Len() == 0 {
			sep = ""
		}
		if colLen(b.String())+colLen(sep)+colLen(chip) > maxW {
			break
		}
		b.WriteString(sep)
		b.WriteString(chip)
	}
	return b.String()
}

// drawDetail is three plain, unboxed rows below the settings pane: the
// selected setting's "label = value", then either a meter bar
// (rangeOf), the full list of preset/theme choices (choices), or
// nothing (a plain toggle's value already says it all), then the help
// text — replaced by the last status message once one is set.
func (u *ui) drawDetail(b *strings.Builder, y0, cols int) {
	s := u.cur()
	if s == nil {
		return
	}
	u.row(b, y0, sgrBold+" "+s.label+" = "+s.get(&u.cfg)+sgrReset)

	switch {
	case s.rangeOf != nil:
		v, lo, hi := s.rangeOf(&u.cfg)
		frac := 0.0
		if hi > lo {
			frac = (v - lo) / (hi - lo)
		}
		mw := clampInt(cols-4, 4, 40)
		u.row(b, y0+1, " "+meterStr(frac, mw))
	case s.choices != nil:
		u.row(b, y0+1, sgrDim+" "+choiceChips(s.choices(), s.get(&u.cfg), cols-2)+sgrReset)
	}

	help := s.help
	if u.status != "" {
		help = u.status
	}
	u.row(b, y0+2, sgrDim+" "+trunc(help, cols-2)+sgrReset)
}

// hex3 formats a linear-light color as an "#RRGGBB" sRGB hex string —
// the color-preview table's value column.
func hex3(c [3]float32) string {
	return fmt.Sprintf("#%02X%02X%02X", srgbByte(c[0]), srgbByte(c[1]), srgbByte(c[2]))
}

// drawColorPreview is the fonts & theme tab's "little preview at the
// bottom" — unboxed, Claude Code-simple: a compact swatch table of the
// theme's own colors (or, for a monochrome theme, its phosphor ramp)
// followed by one line of sample prompt text rendered in those actual
// colors with a trailing block cursor.
func (u *ui) drawColorPreview(b *strings.Builder, y0, cols int, accent [3]float32) {
	u.at(b, y0, 1, truecolorFg(accent)+sgrDim+" PREVIEW"+sgrReset)
	if !u.cfg.TrueColor {
		u.drawRampPreview(b, y0+1, cols)
		u.drawSampleLine(b, y0+3, cols, u.cfg.Phosphor.High, u.cfg.Phosphor.Low)
		return
	}
	swatchRow := func(y int, roleA string, colA [3]float32, roleB string, colB [3]float32) {
		var sl styledLine
		sl.plain(" " + roleA + " ")
		sl.chunk(truecolorBg(colA)+"  "+sgrReset, 2)
		sl.plain(" " + hex3(colA) + "    " + roleB + " ")
		sl.chunk(truecolorBg(colB)+"  "+sgrReset, 2)
		sl.plain(" " + hex3(colB))
		u.at(b, y, 1, sl.buf.String())
	}
	swatchRow(y0+1, "text  ", u.cfg.Colors.DefaultFg, "bg    ", u.cfg.Colors.DefaultBg)
	swatchRow(y0+2, "accent", u.cfg.Phosphor.High, "glow  ", u.cfg.Phosphor.Low)
	u.drawSampleLine(b, y0+3, cols, u.cfg.Colors.DefaultFg, u.cfg.Colors.DefaultBg)
}

// drawRampPreview renders the monochrome phosphor Low→High ramp as a
// wide gradient bar, for themes with no real per-cell color to swatch.
func (u *ui) drawRampPreview(b *strings.Builder, y, cols int) {
	steps := max(4, cols-4)
	var sl styledLine
	sl.plain(" ")
	for i := range steps {
		t := float32(i) / float32(steps-1)
		c := lerpColor(u.cfg.Phosphor.Low, u.cfg.Phosphor.High, t)
		sl.chunk(truecolorBg(c)+" "+sgrReset, 1)
	}
	u.at(b, y, 1, sl.buf.String())
}

// drawSampleLine renders one line of sample prompt text in fg-on-bg plus
// a trailing reverse-video block cursor — a minimal stand-in for "what a
// real shell line looks like in this theme", in the same spirit as
// Claude Code's own simple, low-chrome preview treatments.
func (u *ui) drawSampleLine(b *strings.Builder, y, cols int, fg, bg [3]float32) {
	sample := " tubeless ❯ echo hello, world"
	innerW := max(0, cols-3)
	line := truecolorBg(bg) + truecolorFg(fg) + colPad(trunc(sample, innerW), innerW) + sgrReset
	u.at(b, y, 2, line+sgrReverse+" "+sgrReset)
}

// row emits one line at y (1-based). The screen is cleared with 2J at the
// top of every redraw, so no per-line erase is needed — and an ESC[K right
// after a full-width line would wipe its last cell.
func (u *ui) row(b *strings.Builder, y int, content string) {
	fmt.Fprintf(b, "\x1b[%d;1H%s", y, content)
}

// revRow emits a full-width reverse bar — reserved for states that should
// actually grab attention (the "window too small" warning), not routine
// chrome.
func (u *ui) revRow(b *strings.Builder, y int, s string) {
	u.row(b, y, sgrReverse+colPad(trunc(s, u.cols), u.cols)+sgrReset)
}

// footerRow emits a full-width dim, non-inverted status line — a quieter
// footer than a solid reverse-video block, closer to Claude Code's own
// status line styling.
func (u *ui) footerRow(b *strings.Builder, y int, s string) {
	u.row(b, y, sgrDim+colPad(trunc(s, u.cols), u.cols)+sgrReset)
}

// meterStr renders a horizontal block bar for frac in [0,1].
func meterStr(frac float64, w int) string {
	if w < 1 {
		return ""
	}
	fill := clampInt(int(frac*float64(w)), 0, w)
	out := sgrBold + strings.Repeat("█", fill) + sgrReset
	if rem := w - fill; rem > 0 {
		out += sgrDim + strings.Repeat("░", rem) + sgrReset
	}
	return out
}

// boxTop/boxBottom draw a rounded single-line box spanning `cols` columns,
// with an optional title after the top-left corner.
func boxTop(title string, cols int) string {
	if cols < 4 {
		return "╭╮"
	}
	body := "─"
	if title != "" {
		body = "─ " + title + " "
	}
	// body occupies the interior; top = corner + interior(width cols-2) + corner.
	inner := trunc(body, cols-2)
	if n := colLen(inner); n < cols-2 {
		inner += strings.Repeat("─", cols-2-n)
	}
	return "╭" + inner + "╮"
}

func boxBottom(cols int) string {
	if cols < 2 {
		return "╰╯"
	}
	return "╰" + strings.Repeat("─", cols-2) + "╯"
}

// boxText returns a bordered line with text centred in a w-wide box.
func boxText(t string, w int) string {
	if w < 4 {
		return "││"
	}
	t = trunc(t, w-4)
	tl := colLen(t)
	side := (w - 4 - tl + 1) / 2
	// "│ " (2) + side + t (tl) + trailing + " │" (2) must total exactly w,
	// matching boxBottom's border row — trailing pad is w-4-side-tl, not
	// w-3-side-tl (that extra column made this row one character wider
	// than the box's top/bottom, pushing its right border one column
	// past theirs).
	return "│ " + strings.Repeat(" ", side) + t + strings.Repeat(" ", w-4-side-tl) + " │"
}

func flush(b *strings.Builder) {
	os.Stdout.WriteString(b.String())
}

func colLen(s string) int { return utf8.RuneCountInString(s) }

// colPad pads s with spaces until it occupies exactly w columns (runes).
func colPad(s string, w int) string {
	n := colLen(s)
	if n >= w {
		return s
	}
	return s + strings.Repeat(" ", w-n)
}

func trunc(s string, n int) string {
	if n <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n-1]) + "…"
}

// ---------------- terminal plumbing ----------------
//
// Raw-mode entry/exit and terminal size both go through golang.org/x/term
// rather than hand-rolled ioctl syscalls: the previous implementation used
// Linux's TCGETS/TCSETS ioctl requests and syscall.Termios directly, which
// don't exist under those names on macOS/BSD (they use TIOCGETA/TIOCSETA
// with a differently-laid-out termios struct) — x/term abstracts that
// per-platform difference so this file needs no GOOS-specific variant.

// termSize reports the PTY size in cells.
func termSize() (int, int, error) {
	return term.GetSize(int(os.Stdout.Fd()))
}

// rawTermios holds the terminal state needed to switch stdin to raw mode.
type rawTermios struct {
	fd    int
	state *term.State
}

// rawMode puts stdin into cbreak raw mode (no echo, no line buffering) and
// returns a handle whose restore() puts it back.
func rawMode(fd uintptr) (*rawTermios, error) {
	ifd := int(fd)
	state, err := term.MakeRaw(ifd)
	if err != nil {
		return nil, err
	}
	return &rawTermios{fd: ifd, state: state}, nil
}

func (r *rawTermios) restore() {
	term.Restore(r.fd, r.state)
}

// readKeys forwards every input byte to out until EOF.
func readKeys(f *os.File, out chan<- byte) {
	buf := make([]byte, 256)
	for {
		n, err := f.Read(buf)
		for i := 0; i < n; i++ {
			out <- buf[i]
		}
		if err != nil {
			return
		}
	}
}
