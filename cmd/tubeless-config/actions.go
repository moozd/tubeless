package main

import (
	"sort"
	"strings"

	"github.com/moozd/tubeless/pkg/config"
)

// refreshList points u.list at what the screen shows: the active
// category's rows, or — while a search query is set — every matching
// setting across all categories.
func (u *ui) refreshList() {
	u.scroll = 0
	if u.query == "" {
		u.list = u.cats[u.cat].rows
		return
	}
	u.list = u.searchRows(u.query)
}

// searchRows returns every setting whose category, section, label, key
// or help contains all of the query's words, best label matches first.
func (u *ui) searchRows(query string) []panelRow {
	words := strings.Fields(strings.ToLower(query))
	type hit struct {
		row   panelRow
		score int
	}
	var hits []hit
	for _, cat := range u.cats {
		section := ""
		for _, r := range cat.rows {
			if r.kind == rowSection {
				section = r.section
				continue
			}
			score, ok := matchScore(words, cat.name, section, r.set)
			if !ok {
				continue
			}
			row := panelRow{kind: rowSetting, set: r.set, crumb: cat.name + " › " + section}
			hits = append(hits, hit{row, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	rows := make([]panelRow, len(hits))
	for i, h := range hits {
		rows[i] = h.row
	}
	return rows
}

// matchScore reports whether every word appears somewhere in the
// setting's text; each word found in the label or key scores higher
// than one found only in the help.
func matchScore(words []string, cat, section string, s *setting) (int, bool) {
	near := strings.ToLower(s.label + " " + s.key)
	far := strings.ToLower(cat + " " + section + " " + s.help)
	score := 0
	for _, w := range words {
		switch {
		case strings.Contains(near, w):
			score += 2
		case strings.Contains(far, w):
			score++
		default:
			return 0, false
		}
	}
	return score, true
}

func (u *ui) setQuery(q string) {
	if u.query == "" {
		u.selByCat[u.cat] = u.sel
	}
	u.query = q
	u.sel = 0
	u.refreshList()
	u.firstSetting()
}

func (u *ui) clearSearch() {
	u.searching = false
	if u.query == "" {
		return
	}
	u.query = ""
	u.refreshList()
	u.sel = u.selByCat[u.cat]
	if u.cur() == nil {
		u.firstSetting()
	}
}

func (u *ui) switchCategory(d int) {
	if u.query != "" {
		u.clearSearch()
	}
	u.selByCat[u.cat] = u.sel
	n := len(u.cats)
	u.cat = (u.cat + d + n) % n
	u.refreshList()
	u.sel = u.selByCat[u.cat]
	if u.cur() == nil {
		u.firstSetting()
	}
}

func (u *ui) firstSetting() {
	u.sel = -1
	u.move(1)
}

func (u *ui) lastSetting() {
	u.sel = 0
	u.move(-1)
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
	if u.sel < 0 || u.sel >= len(u.list) || u.list[u.sel].kind != rowSetting {
		return nil
	}
	return u.list[u.sel].set
}

// refreshReference rebuilds what "unmodified" means for the settings on
// the theme and effects axes: the values of the currently named theme
// and preset, so picking nord does not mark every color as changed.
func (u *ui) refreshReference() {
	u.ref = u.base
	if name := u.cfg.Theme; name != "" && name != "custom" {
		applyThemeCfg(&u.ref, name)
	}
	if name := u.cfg.Preset; name != "" && name != "custom" {
		applyEffectsPresetCfg(&u.ref, name)
	}
}

// modified reports whether a setting differs from its reference: the
// factory default, or for an axis setting the named theme/preset it
// came from.
func (u *ui) modified(s *setting) bool {
	if s.noMarker {
		return false
	}
	ref := &u.base
	if s.axis != axisNone && !s.names {
		ref = &u.ref
	}
	return s.get(&u.cfg) != s.get(ref)
}

// adjust applies one step to the selected setting, then flips its axis
// to "custom" if hand-editing it leaves the named theme/preset behind.
// The name rows themselves already set a real name via applyStep.
func (u *ui) adjust(d int) {
	s := u.cur()
	if s == nil {
		return
	}
	if needsFont := s.applyStep(&u.cfg, d); needsFont {
		u.status = "font/atlas changed — press s to save"
	}
	if !s.names {
		u.flipToCustom(s.axis)
	}
	u.dirty()
}

func (u *ui) flipToCustom(a axis) {
	switch a {
	case axisEffects:
		u.cfg.Preset = "custom"
	case axisTheme:
		u.cfg.Theme = "custom"
	}
}

// resetAxis snaps the selected setting's axis back to its named
// preset/theme — modern/rosepine if that axis is currently "custom".
func (u *ui) resetAxis() {
	s := u.cur()
	if s == nil || s.axis == axisNone {
		u.status = "nothing to reset here — r resets a theme or effects preset"
		return
	}
	if s.axis == axisEffects {
		name := namedOr(u.cfg.Preset, "modern")
		applyEffectsPresetCfg(&u.cfg, name)
		u.status = "effects reset to " + name
	} else {
		name := namedOr(u.cfg.Theme, "rosepine")
		applyThemeCfg(&u.cfg, name)
		u.status = "theme reset to " + name
	}
	u.dirty()
}

func namedOr(name, fallback string) string {
	if name == "" || name == "custom" {
		return fallback
	}
	return name
}

// save writes u.cfg to the live config.toml. The running tubeless host
// watches that one file, so writing it is the only activation step.
func (u *ui) save() {
	if err := config.Save(u.path, u.cfg); err != nil {
		u.status = "save failed: " + err.Error()
		return
	}
	u.saved = u.cfg
	u.unsaved = false
	u.status = "saved → " + u.path
}

// dirty marks u.cfg as having an unsaved edit. Nothing reaches disk
// until the user presses s, so arrow-key nudges never take effect in the
// running terminal on their own.
func (u *ui) dirty() {
	u.unsaved = true
}
