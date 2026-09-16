package render

import (
	"testing"

	"github.com/moozd/tubeless/pkg/config"
	"github.com/moozd/tubeless/pkg/font"
	"github.com/moozd/tubeless/pkg/screen"
)

func mkLigCell(r rune, fg [3]float32) screen.Cell {
	return screen.Cell{Rune: r, Attr: screen.Attr{FgSet: true, FgRGB: fg}}
}

// TestMatchLigatureLongestMatchWins guards the core greedy-match contract:
// a shorter key not being the best available (here "=>" ) must not stop
// matchLigature from finding a longer one ("=>>") starting at the same
// cell — real fonts define both a short and a longer, more specific
// ligature over overlapping prefixes.
func TestMatchLigatureLongestMatchWins(t *testing.T) {
	atlas := &font.Atlas{Ligatures: map[string]font.Ligature{
		"=>":  {Glyph: font.Glyph{X: 1, Y: 2, W: 20, H: 10}, Cells: 2},
		"=>>": {Glyph: font.Glyph{X: 3, Y: 4, W: 30, H: 10}, Cells: 3},
	}}
	cfg := config.Config{TrueColor: true}
	red := [3]float32{1, 0, 0}
	grid := [][]screen.Cell{{mkLigCell('=', red), mkLigCell('>', red), mkLigCell('>', red)}}

	lig, ok := matchLigature(atlas, grid, 3, 0, 0, red, cfg, Selection{})
	if !ok || lig.Cells != 3 {
		t.Fatalf("want the 3-cell match, got %+v ok=%v", lig, ok)
	}
}

// TestMatchLigatureStopsAtColorChange guards against a merged glyph
// spanning cells that actually need two different colors — a single
// coverage bitmap can only ever be tinted one color, so a foreground
// change partway through a would-be ligature run must break the match.
func TestMatchLigatureStopsAtColorChange(t *testing.T) {
	atlas := &font.Atlas{Ligatures: map[string]font.Ligature{
		"=>>": {Glyph: font.Glyph{X: 0, Y: 0, W: 30, H: 10}, Cells: 3},
	}}
	cfg := config.Config{TrueColor: true}
	red, blue := [3]float32{1, 0, 0}, [3]float32{0, 0, 1}
	grid := [][]screen.Cell{{mkLigCell('=', red), mkLigCell('>', blue), mkLigCell('>', red)}}

	if _, ok := matchLigature(atlas, grid, 3, 0, 0, red, cfg, Selection{}); ok {
		t.Fatal("must not match across a foreground color change")
	}
}

// TestMatchLigatureStopsAtStyleChange mirrors the color-change guard for
// Bold/Italic — those pick a different atlas (and therefore a different
// Ligatures map) entirely, so a run must never cross that boundary.
func TestMatchLigatureStopsAtStyleChange(t *testing.T) {
	atlas := &font.Atlas{Ligatures: map[string]font.Ligature{
		"=>": {Glyph: font.Glyph{X: 0, Y: 0, W: 20, H: 10}, Cells: 2},
	}}
	cfg := config.Config{TrueColor: true}
	red := [3]float32{1, 0, 0}
	bold := mkLigCell('>', red)
	bold.Attr.Bold = true
	grid := [][]screen.Cell{{mkLigCell('=', red), bold}}

	if _, ok := matchLigature(atlas, grid, 2, 0, 0, red, cfg, Selection{}); ok {
		t.Fatal("must not match across a bold style change")
	}
}

// TestMatchLigatureNoLigaturesIsANoOp guards the zero-cost path most
// fonts (and font.ligatures=false) take: an empty/nil Ligatures map must
// short-circuit rather than doing any per-cell comparison work.
func TestMatchLigatureNoLigaturesIsANoOp(t *testing.T) {
	atlas := &font.Atlas{}
	cfg := config.Config{TrueColor: true}
	red := [3]float32{1, 0, 0}
	grid := [][]screen.Cell{{mkLigCell('=', red), mkLigCell('>', red)}}

	if _, ok := matchLigature(atlas, grid, 2, 0, 0, red, cfg, Selection{}); ok {
		t.Fatal("an empty Ligatures map must never match")
	}
}
