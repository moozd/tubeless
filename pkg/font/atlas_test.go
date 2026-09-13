package font

import "testing"

func TestBuildProducesCoverage(t *testing.T) {
	atlas, err := Build(DefaultFontBytes(), []rune("Ag@"), 20, 1.0, 1, 1.0, nil, 0)
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
	atlas, err := Build(DefaultFontBytes(), []rune{'A', 0x1F600}, 20, 1.0, 1, 1.0, nil, 0)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, ok := atlas.Glyphs['A']; !ok {
		t.Fatal("missing glyph A")
	}
}

// TestLineHeightKeepsSpritesEdgeToEdge guards against the obvious way a
// line-height feature could break box drawing and solid blocks: if the
// extra space it adds were reserved as margin around a sprite instead of
// baked into the cellH the sprite is drawn to fill, a vertical box-drawing
// line or a full block would stop reaching the cell's top/bottom edges —
// leaving a visible gap between terminal rows. Build always draws sprites
// into the final (already-scaled) cellH, so this should hold at any
// line-height.
func TestLineHeightKeepsSpritesEdgeToEdge(t *testing.T) {
	for _, lh := range []float64{0.8, 1.0, 1.5, 2.0} {
		atlas, err := Build(DefaultFontBytes(), []rune{0x2502, 0x2588}, 20, 1.0, 1, lh, nil, 0)
		if err != nil {
			t.Fatalf("Build(lineHeight=%v): %v", lh, err)
		}
		for _, r := range []rune{0x2502, 0x2588} { // │ and █
			g, ok := atlas.Glyphs[r]
			if !ok {
				t.Fatalf("lineHeight=%v: missing sprite for %q", lh, r)
			}
			top, bottom := g.Y, g.Y+g.H-1
			edgeInked := false
			for x := g.X; x < g.X+g.W; x++ {
				if atlas.Image.AlphaAt(x, top).A > 0 || atlas.Image.AlphaAt(x, bottom).A > 0 {
					edgeInked = true
					break
				}
			}
			if !edgeInked {
				t.Fatalf("lineHeight=%v: %q has no ink on its top or bottom edge row (cellH=%d)", lh, r, g.H)
			}
		}
	}
}

// TestLineHeightScalesCellHeight confirms the multiplier actually reaches
// the atlas's cell size, symmetric around 1.0 (no font-specific rounding
// surprise, since 1.0 must be a no-op relative to the unscaled metrics).
func TestLineHeightScalesCellHeight(t *testing.T) {
	base, err := Build(DefaultFontBytes(), []rune("A"), 40, 1.0, 1, 1.0, nil, 0)
	if err != nil {
		t.Fatalf("Build(lineHeight=1.0): %v", err)
	}
	tall, err := Build(DefaultFontBytes(), []rune("A"), 40, 1.0, 1, 1.5, nil, 0)
	if err != nil {
		t.Fatalf("Build(lineHeight=1.5): %v", err)
	}
	if tall.CellHeight <= base.CellHeight {
		t.Fatalf("lineHeight=1.5 cellH %d not taller than lineHeight=1.0 cellH %d", tall.CellHeight, base.CellHeight)
	}
	if tall.CellWidth != base.CellWidth {
		t.Fatalf("line height changed CellWidth: %d vs %d", tall.CellWidth, base.CellWidth)
	}
}

// TestBuildFillsGapsFromFallback guards the fix for icons disappearing
// whenever a user configures a font.family that isn't a Nerd Font, or is
// a different one than the bundled default: a codepoint missing from the
// primary font's own rune list must still show up in the atlas — pulled
// from fallbackBytes — rather than rendering blank. There's no second
// real font file to test against here, so this isolates the merge logic
// itself: `icon` is a real glyph in the bundled font, deliberately left
// out of the `runes` list Build is given for the "primary" pass, proving
// it still reaches the atlas only because fallbackBytes covers it.
func TestBuildFillsGapsFromFallback(t *testing.T) {
	all, err := EnumerateRunes(DefaultFontBytes())
	if err != nil {
		t.Fatalf("EnumerateRunes: %v", err)
	}
	var icon rune = -1
	for _, r := range all {
		if r >= 0xE000 && r <= 0xF8FF { // Nerd Font Private Use Area
			icon = r
			break
		}
	}
	if icon == -1 {
		t.Skip("bundled font has no PUA icon glyphs to test against")
	}

	withFallback, err := Build(DefaultFontBytes(), []rune{'A'}, 20, 1.0, 1, 1.0, DefaultFontBytes(), 0)
	if err != nil {
		t.Fatalf("Build with fallback: %v", err)
	}
	g, ok := withFallback.Glyphs[icon]
	if !ok {
		t.Fatalf("icon %U missing even with fallbackBytes set", icon)
	}
	if !hasCoverage(withFallback, g) {
		t.Fatalf("icon %U has no ink", icon)
	}

	withoutFallback, err := Build(DefaultFontBytes(), []rune{'A'}, 20, 1.0, 1, 1.0, nil, 0)
	if err != nil {
		t.Fatalf("Build without fallback: %v", err)
	}
	if _, ok := withoutFallback.Glyphs[icon]; ok {
		t.Fatalf("icon %U present with fallbackBytes nil — test no longer isolates the merge path", icon)
	}
}

// TestBuildRejectsOversizedAtlas guards the actual bug behind "all text
// disappears" at a high atlas.scale: the bundled Nerd Font alone
// enumerates to ~12,000 glyphs, and a high enough atlas.scale packs them
// into a texture that crosses common GPUs' GL_MAX_TEXTURE_SIZE (16384).
// Before this check, Build "succeeded" with an oversized image.Alpha,
// and the failure only surfaced later as a silent glTexImage2D error
// with nothing readable in the process's own control flow — text just
// vanished. maxTextureSize deliberately tiny here (a real cell alone
// already exceeds it) keeps this fast and independent of the actual
// rune count or any real GPU.
func TestBuildRejectsOversizedAtlas(t *testing.T) {
	_, err := Build(DefaultFontBytes(), []rune("A"), 200, 1.0, 1, 1.0, nil, 10)
	if err == nil {
		t.Fatal("Build did not reject an atlas far larger than maxTextureSize")
	}
}

// TestResizeCoverageSqueezesWidthWithoutCroppingOrChangingHeight
// reproduces a real user report: an italic cut's slant routinely widens
// a letter's rendered bitmap past the monospace cell width while its
// height stays well within the cell — a width-only overflow. Two
// approaches were tried and rejected before this one: a uniform
// (aspect-preserving) shrink also shrank the letter's height, reading as
// that specific letter being smaller than its neighbors; leaving the
// bitmap alone and letting a hard per-pixel clip drop whatever crossed
// the cell edge cropped chunks out of the letter, which read as broken
// glyphs. resizeCoverage's independent per-axis resize (only narrowing
// the overflowing axis) must square (h unaffected) and must not crop —
// a uniform fully-covered source resized down should stay fully covered
// throughout, never leaving a destination pixel untouched at 0.
func TestResizeCoverageSqueezesWidthWithoutCroppingOrChangingHeight(t *testing.T) {
	const w, h = 10, 4
	pix := make([]byte, w*h)
	for i := range pix {
		pix[i] = 255
	}
	out, ow, oh := resizeCoverage(pix, w, h, 6, h)
	if oh != h {
		t.Fatalf("oh = %d, want unchanged %d (height must never be touched by a width-only overflow)", oh, h)
	}
	if ow != 6 {
		t.Fatalf("ow = %d, want 6", ow)
	}
	if len(out) != ow*oh {
		t.Fatalf("len(out) = %d, want %d", len(out), ow*oh)
	}
	for i, v := range out {
		if v != 255 {
			t.Fatalf("out[%d] = %d, want 255 (box-filtered from a uniform fully-covered source — a crop would leave some destination pixels untouched at 0)", i, v)
		}
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
