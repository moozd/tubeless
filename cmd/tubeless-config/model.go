package main

import (
	"strings"

	"github.com/moozd/tubeless/pkg/config"
)

// ui is the whole screen's state: the config being edited, the sidebar
// categories, and whichever overlay (search, picker) is capturing input.
type ui struct {
	cfg config.Config
	// saved is what the host is rendering with: the chrome is painted from
	// it, so browsing previews never recolor the screen around them.
	saved config.Config
	base  config.Config // factory defaults, for the modified markers
	ref   config.Config // base reseeded with the active theme and preset
	path  string
	cols  int
	rows  int

	cats     []category
	cat      int
	list     []panelRow // rows on screen: the active category, or search hits
	sel      int
	selByCat []int
	scroll   int

	query     string
	searching bool // typing into the search box

	picker *picker

	status  string
	esc     []byte
	unsaved bool
}

// category is one sidebar entry and the settings under it.
type category struct {
	name string
	rows []panelRow
}

type rowKind uint8

const (
	rowSection rowKind = iota
	rowSetting
)

type panelRow struct {
	kind    rowKind
	section string
	crumb   string // "Category › Section", set on search hits only
	set     *setting
}

// axis says which of the two independent look axes a setting belongs to.
// Hand-editing a setting on an axis flips that axis's name to "custom".
type axis uint8

const (
	axisNone axis = iota
	axisTheme
	axisEffects
)

// setting is one adjustable row. rangeOf, when set, draws a meter on the
// selected row. pick, when set, makes enter open a searchable list.
type setting struct {
	key       string
	label     string
	help      string
	get       func(*config.Config) string
	applyStep func(*config.Config, int) (needsFont bool)
	rangeOf   func(*config.Config) (v, min, max float64)
	pick      *pickSpec
	axis      axis
	// names marks the row that holds the axis's own name (theme/preset):
	// stepping it sets a real name, so it never flips to "custom".
	names bool
	// noMarker skips the modified dot, for rows whose factory default
	// is not meaningful to compare against.
	noMarker bool
	// compact rows take one line until selected (palette slots, color
	// channels), keeping long lists short.
	compact bool
	// swatch, when set, is a live color this row represents.
	swatch func(*config.Config) [3]float32
}

// pickSpec describes the searchable list a row opens on enter.
type pickSpec struct {
	title   string
	items   func() ([]string, error)
	current func(*config.Config) string
	apply   func(*config.Config, string)
	preview bool // apply live while browsing, restore on cancel
	free    bool // enter with no match takes the typed text
	font    bool // applying rebuilds the font atlas
}

func newUI(cfg config.Config, path string, cols, rows int) *ui {
	base := config.Default()
	cats := buildCategories()
	u := &ui{
		cfg: cfg, saved: cfg, base: base, path: path, cols: cols, rows: rows,
		cats: cats, selByCat: make([]int, len(cats)),
	}
	u.refreshList()
	u.firstSetting()
	return u
}

// configSnapshot is the state a cancelled preview returns to.
type configSnapshot struct {
	cfg     config.Config
	unsaved bool
}

func (u *ui) snapshot() configSnapshot {
	return configSnapshot{cfg: u.cfg, unsaved: u.unsaved}
}

func (u *ui) restore(s configSnapshot) {
	u.cfg, u.unsaved = s.cfg, s.unsaved
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
