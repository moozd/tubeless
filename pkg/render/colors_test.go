package render

import (
	"testing"

	"github.com/moozd/tubeless/pkg/config"
	"github.com/moozd/tubeless/pkg/screen"
)

func TestBlockRectEdgesJoinDifferentBlockRunes(t *testing.T) {
	cfg := config.Default()
	cfg.Rounding.Radius = 4
	attr := screen.Attr{FgSet: true, Fg: 1}
	grid := [][]screen.Cell{{
		{Rune: '█', Attr: attr},
		{Rune: '▌', Attr: attr},
	}}
	fgCache, bgCache := blockColorCaches(grid, cfg)
	fg, _ := cellColors(attr, cfg)

	e := blockRectEdges(grid, 2, 1, fgCache, bgCache, cfg, 0, 0, '█', fg, 0, 0, 1, 1)

	if !e.ContRight {
		t.Fatal("full block did not continue into left-half block")
	}
	if e.Radii[1] != 0 || e.Radii[2] != 0 {
		t.Fatalf("right joined corners = %v, want both 0", e.Radii)
	}
}

func TestBlockRectEdgesPartialJoinKeepsUnjoinedCornerRounded(t *testing.T) {
	cfg := config.Default()
	cfg.Rounding.Radius = 4
	attr := screen.Attr{FgSet: true, Fg: 1}
	grid := [][]screen.Cell{{
		{Rune: '█', Attr: attr},
		{Rune: '▖', Attr: attr},
	}}
	fgCache, bgCache := blockColorCaches(grid, cfg)
	fg, _ := cellColors(attr, cfg)

	e := blockRectEdges(grid, 2, 1, fgCache, bgCache, cfg, 0, 0, '█', fg, 0, 0, 1, 1)

	if e.ContRight {
		t.Fatal("partial edge join expanded the whole right edge")
	}
	if e.Radii[1] != cfg.Rounding.Radius {
		t.Fatalf("top-right radius = %v, want %v", e.Radii[1], cfg.Rounding.Radius)
	}
	if e.Radii[2] != 0 {
		t.Fatalf("bottom-right radius = %v, want 0", e.Radii[2])
	}
}

func TestBgRectEdgesSuppressesDiagonalInternalCorner(t *testing.T) {
	cfg := config.Default()
	cfg.Rounding.Radius = 4
	attr := screen.Attr{BgSet: true, Bg: 0.5}
	grid := [][]screen.Cell{
		{{Rune: ' ', Attr: attr}, {Rune: ' ', Attr: screen.Attr{}}},
		{{Rune: ' ', Attr: screen.Attr{}}, {Rune: ' ', Attr: attr}},
	}
	fgCache, bgCache := blockColorCaches(grid, cfg)
	_, bg := cellColors(attr, cfg)

	e := bgRectEdges(grid, 2, 2, fgCache, bgCache, cfg, 0, 0, bg)

	if e.Radii[2] != 0 {
		t.Fatalf("diagonal internal radius = %v, want 0", e.Radii[2])
	}
}

func TestBlockRectEdgesSuppressesDiagonalStructuralCorner(t *testing.T) {
	cfg := config.Default()
	cfg.Rounding.Radius = 4
	attr := screen.Attr{FgSet: true, Fg: 1}
	grid := [][]screen.Cell{
		{{Rune: '█', Attr: attr}, {Rune: ' ', Attr: screen.Attr{}}},
		{{Rune: ' ', Attr: screen.Attr{}}, {Rune: '█', Attr: attr}},
	}
	fgCache, bgCache := blockColorCaches(grid, cfg)
	fg, _ := cellColors(attr, cfg)

	e := blockRectEdges(grid, 2, 2, fgCache, bgCache, cfg, 0, 0, '█', fg, 0, 0, 1, 1)

	if e.Radii[2] != 0 {
		t.Fatalf("diagonal structural radius = %v, want 0", e.Radii[2])
	}
}

func blockColorCaches(grid [][]screen.Cell, cfg config.Config) ([][3]float32, [][3]float32) {
	cols, rows := len(grid[0]), len(grid)
	fgCache := make([][3]float32, cols*rows)
	bgCache := make([][3]float32, cols*rows)
	for y, row := range grid {
		for x, cell := range row {
			fgCache[y*cols+x], bgCache[y*cols+x] = cellColors(cell.Attr, cfg)
		}
	}
	return fgCache, bgCache
}
