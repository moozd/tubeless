package main

import (
	"slices"
	"sort"
	"strings"
)

// picker is the searchable list a row opens on enter: themes, effects
// presets, font families. A previewing picker applies the highlighted
// choice live and restores the snapshot on cancel.
type picker struct {
	spec     *pickSpec
	items    []string
	err      string
	query    string
	matches  []string
	sel      int
	snapshot configSnapshot
}

func (u *ui) openPicker(s *setting) {
	spec := s.pick
	items, err := spec.items()
	p := &picker{spec: spec, items: items, snapshot: u.snapshot()}
	if err != nil {
		p.err = err.Error()
	}
	p.refresh()
	if i := slices.Index(p.matches, spec.current(&u.cfg)); i >= 0 {
		p.sel = i
	}
	u.picker = p
}

// refresh re-filters items by query (fuzzyMatch), tightest match first.
func (p *picker) refresh() {
	p.matches = p.matches[:0]
	scores := map[string]int{}
	for _, name := range p.items {
		if score, ok := fuzzyMatch(p.query, name); ok {
			p.matches = append(p.matches, name)
			scores[name] = score
		}
	}
	sort.SliceStable(p.matches, func(i, j int) bool {
		return scores[p.matches[i]] < scores[p.matches[j]]
	})
	p.sel = clampInt(p.sel, 0, max(0, len(p.matches)-1))
}

func (u *ui) pickerMove(d int) {
	p := u.picker
	if len(p.matches) == 0 {
		return
	}
	p.sel = clampInt(p.sel+d, 0, len(p.matches)-1)
	u.previewPick()
}

func (u *ui) pickerType(b byte) {
	p := u.picker
	switch {
	case b == 0x7f || b == '\b':
		if rs := []rune(p.query); len(rs) > 0 {
			p.query = string(rs[:len(rs)-1])
		}
	case b >= 0x20 && b < 0x7f:
		p.query += string(rune(b))
	default:
		return
	}
	p.sel = 0
	p.refresh()
	u.previewPick()
}

// previewPick applies the highlighted choice to the live config, from
// the snapshot each time so previews never stack.
func (u *ui) previewPick() {
	p := u.picker
	if !p.spec.preview || len(p.matches) == 0 {
		return
	}
	u.restore(p.snapshot)
	p.spec.apply(&u.cfg, p.matches[p.sel])
}

// commitPicker keeps the highlighted choice — or, for a free-text
// picker with nothing matching (or no fontconfig at all), whatever was
// typed, so an exact name always works.
func (u *ui) commitPicker() {
	p := u.picker
	choice := strings.TrimSpace(p.query)
	if p.sel < len(p.matches) {
		choice = p.matches[p.sel]
	} else if !p.spec.free {
		u.status = "no match"
		return
	}
	u.restore(p.snapshot)
	p.spec.apply(&u.cfg, choice)
	if p.spec.font {
		u.status = "font family changed — press s to save"
	}
	u.picker = nil
	u.dirty()
}

func (u *ui) cancelPicker() {
	u.restore(u.picker.snapshot)
	u.picker = nil
	u.status = "cancelled"
}
