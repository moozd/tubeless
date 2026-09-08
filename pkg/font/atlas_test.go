package font

import "testing"

func TestBuildProducesCoverage(t *testing.T) {
	atlas, err := Build(DefaultFontBytes(), []rune("Ag@"), 20, 1.0)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if atlas.CellWidth <= 0 || atlas.CellHeight <= 0 {
		t.Fatalf("bad cell size: %dx%d", atlas.CellWidth, atlas.CellHeight)
	}
	g, ok := atlas.Glyphs['A']
	if !ok {
		t.Fatal("missing glyph A")
	}
	if !hasCoverage(atlas, g) {
		t.Fatal("glyph A has no ink at all")
	}
}

func TestBuildSkipsMissingGlyphsGracefully(t *testing.T) {
	// U+1F600 (an emoji) is very unlikely to be in a plain monospace TTF;
	// this must not fail the whole atlas build.
	atlas, err := Build(DefaultFontBytes(), []rune{'A', 0x1F600}, 20, 1.0)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, ok := atlas.Glyphs['A']; !ok {
		t.Fatal("missing glyph A")
	}
}

func hasCoverage(atlas *Atlas, g Glyph) bool {
	for y := g.Y; y < g.Y+g.H; y++ {
		for x := g.X; x < g.X+g.W; x++ {
			if atlas.Image.AlphaAt(x, y).A > 0 {
				return true
			}
		}
	}
	return false
}
