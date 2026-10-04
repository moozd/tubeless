package main

import (
	"testing"

	"github.com/moozd/tubeless/pkg/config"
)

func testUI() *ui {
	return newUI(config.Default(), "unused.toml", 120, 36)
}

// settingByKey finds a setting across every category.
func settingByKey(t *testing.T, u *ui, key string) *setting {
	t.Helper()
	for _, cat := range u.cats {
		for _, r := range cat.rows {
			if r.kind == rowSetting && r.set.key == key {
				return r.set
			}
		}
	}
	t.Fatalf("no setting %q", key)
	return nil
}

func TestEffectsPresetLeavesTheTheme(t *testing.T) {
	cfg := config.Default()
	want := cfg
	for _, name := range config.EffectsPresetNames() {
		applyEffectsPresetCfg(&cfg, name)
		if cfg.Theme != want.Theme || cfg.Colors != want.Colors || cfg.Phosphor != want.Phosphor || cfg.TrueColor != want.TrueColor {
			t.Errorf("preset %q changed the theme axis", name)
		}
	}
}

func TestThemeLeavesTheEffects(t *testing.T) {
	cfg := config.Default()
	applyEffectsPresetCfg(&cfg, "neon")
	want := cfg
	for _, name := range config.ThemeNames() {
		applyThemeCfg(&cfg, name)
		if cfg.Preset != want.Preset || cfg.Surface != want.Surface || cfg.Face != want.Face || cfg.CRT != want.CRT {
			t.Errorf("theme %q changed the effects axis", name)
		}
	}
}

func TestEditingOneAxisLeavesTheOtherNamed(t *testing.T) {
	u := testUI()
	applyThemeCfg(&u.cfg, "nord")
	applyEffectsPresetCfg(&u.cfg, "ibm-5153")
	u.selectKey(t, "blur.radius")
	u.adjust(1)
	if u.cfg.Preset != "custom" || u.cfg.Theme != "nord" {
		t.Errorf("got preset %q theme %q, want custom/nord", u.cfg.Preset, u.cfg.Theme)
	}
}

// selectKey searches for a key and selects its first hit.
func (u *ui) selectKey(t *testing.T, key string) {
	t.Helper()
	u.setQuery(key)
	if u.cur() == nil || u.cur().key != key {
		t.Fatalf("search %q did not select it", key)
	}
}

func TestSearchMatchesAllWords(t *testing.T) {
	u := testUI()
	rows := u.searchRows("text glow strength")
	if len(rows) == 0 || rows[0].set.key != "text_glow.strength" {
		t.Fatalf("top hit for %q = %+v", "text glow strength", rows)
	}
	if rows := u.searchRows("zzzz-no-such"); len(rows) != 0 {
		t.Errorf("nonsense query matched %d rows", len(rows))
	}
}

func TestSearchRanksLabelHitsAboveHelpHits(t *testing.T) {
	u := testUI()
	rows := u.searchRows("scanlines")
	if len(rows) < 2 || rows[0].crumb != "CRT › Scanlines" {
		t.Fatalf("first hit crumb = %q", rows[0].crumb)
	}
}

func TestModifiedMarkerFollowsTheNamedTheme(t *testing.T) {
	u := testUI()
	ch := settingByKey(t, u, "theme.text.r")
	applyThemeCfg(&u.cfg, "nord")
	u.refreshReference()
	if u.modified(ch) {
		t.Error("a channel of the named theme reads as modified")
	}
	u.selectKey(t, "theme.text.r")
	u.adjust(1)
	u.refreshReference()
	if !u.modified(ch) {
		t.Error("a hand-edited channel is not marked modified")
	}
}

func TestModifiedMarkerOnPlainSetting(t *testing.T) {
	u := testUI()
	size := settingByKey(t, u, "font.size")
	if u.modified(size) {
		t.Fatal("factory font size reads as modified")
	}
	u.cfg.Font.Size++
	if !u.modified(size) {
		t.Error("changed font size is not marked modified")
	}
}

func TestPickerCancelRestoresPreview(t *testing.T) {
	u := testUI()
	before := u.cfg
	u.selectKey(t, "theme")
	u.openPicker(u.cur())
	u.pickerMove(3)
	if u.cfg.Theme == before.Theme {
		t.Fatal("preview did not apply")
	}
	u.cancelPicker()
	if u.cfg != before || u.unsaved {
		t.Error("cancel did not restore the config")
	}
}

func TestPickerCommitKeepsChoice(t *testing.T) {
	u := testUI()
	u.selectKey(t, "preset")
	u.openPicker(u.cur())
	u.pickerType('n')
	u.pickerType('e')
	u.pickerType('o')
	u.commitPicker()
	if u.cfg.Preset != "neon" || !u.unsaved || u.picker != nil {
		t.Errorf("preset %q unsaved %v picker %v", u.cfg.Preset, u.unsaved, u.picker)
	}
	if u.cfg.Theme != "rosepine" {
		t.Errorf("picking a preset changed the theme to %q", u.cfg.Theme)
	}
}

func TestResetAxisOnlyTouchesItsOwnAxis(t *testing.T) {
	u := testUI()
	applyThemeCfg(&u.cfg, "dracula")
	u.cfg.Preset = "custom"
	u.cfg.Blur.Radius = 7
	u.selectKey(t, "blur.radius")
	u.resetAxis()
	if u.cfg.Theme != "dracula" || u.cfg.Preset != "modern" {
		t.Errorf("got theme %q preset %q", u.cfg.Theme, u.cfg.Preset)
	}
}

func TestWrapText(t *testing.T) {
	got := wrapText("one two three four five", 9, 2)
	if len(got) != 2 || got[0] != "one two" {
		t.Errorf("wrapText = %q", got)
	}
	if last := got[len(got)-1]; colLen(last) > 9 {
		t.Errorf("last line %q exceeds width", last)
	}
}

func TestClearSearchKeepsASettingSelected(t *testing.T) {
	u := testUI()
	u.switchCategory(1)
	u.setQuery("blur")
	u.clearSearch()
	if u.cur() == nil {
		t.Fatal("no setting selected after leaving search")
	}
	if u.query != "" || u.cats[u.cat].name != "Font" {
		t.Errorf("query %q category %q", u.query, u.cats[u.cat].name)
	}
}

func TestChromeFollowsSavedConfigNotPreview(t *testing.T) {
	u := testUI()
	before := u.skin()
	u.selectKey(t, "theme")
	u.openPicker(u.cur())
	u.pickerType('m')
	u.pickerType('s')
	u.pickerType('d')
	if u.cfg.Theme == u.saved.Theme {
		t.Fatal("preview did not change the working theme")
	}
	if u.skin() != before {
		t.Error("browsing a theme recolored the chrome")
	}
}
