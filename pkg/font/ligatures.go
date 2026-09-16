package font

import (
	"fmt"
	"image"
	"image/draw"
)

// Ligature is one Atlas.Ligatures entry: an already-fit-and-packed glyph
// plus how many grid columns wide it must be drawn across — the exact
// number of cells the rune sequence it replaces occupied, unlike
// WideGlyphs' variable, fit-driven width. CellPass stretches its quad to
// exactly Cells*cellW rather than deriving a ratio the way it does for
// WideGlyphs, since a ligature must land flush on the grid columns the
// characters it replaces used to occupy.
type Ligature struct {
	Glyph
	Cells int
}

// ligatureAlphabet is the candidate corpus discoverLigatures searches
// over: every printable ASCII character except space. This isn't a list
// of known ligature sequences (nothing here is FiraCode-specific, or
// specific to any font) — it's the character set real programming-
// ligature fonts draw their triggers from (operators, punctuation,
// repeated letters like the "www" ligature), searched combinatorially so
// discovery finds whatever the loaded font actually defines rather than
// guessing sequences ahead of time.
func ligatureAlphabet() []rune {
	alphabet := make([]rune, 0, '~'-'!'+1)
	for r := rune('!'); r <= '~'; r++ {
		alphabet = append(alphabet, r)
	}
	return alphabet
}

// maxLigatureLen bounds how long a discovered sequence can grow.
// Generous headroom over any known real ligature (FiraCode's longest
// are 4-5 characters; even an unusually long one like "<!--" or "###!"
// fits well inside this) while keeping discoverLigatures' search bounded.
const maxLigatureLen = 6

// ligCandidate is one still-unpacked Ligature discovery result, keyed by
// the literal rune sequence it replaces — mirrors wideIcon's role for
// buildWideGlyphs.
type ligCandidate struct {
	key       string
	pix       []byte
	w, h      int
	left, top int
	cells     int
	slotW     int // cells*cellW, this candidate's fixed packed width
}

// buildLigatures discovers and rasterizes every ligature fontBytes's own
// font actually collapses to a single glyph (see discoverLigatures),
// packing them into atlas.Ligatures the same way buildWideGlyphs packs
// Atlas.WideGlyphs — a second region shelf-packed onto the bottom of
// atlas.Image. A no-op (atlas.Ligatures left as an empty, non-nil map)
// whenever the font defines no ligatures the search corpus reaches.
func buildLigatures(atlas *Atlas, fontBytes []byte, face *ftFace, cellW, cellH, ascender int, gamma float64, glyphPadding, maxTextureSize int) error {
	atlas.Ligatures = map[string]Ligature{}

	shaper := newHBShaper(fontBytes)
	defer shaper.free()
	rules := discoverLigatures(shaper)
	if len(rules) == 0 {
		return nil
	}

	var candidates []ligCandidate
	for key, gid := range rules {
		pix, w, h, left, top, ok := face.glyphBitmapByIndex(gid)
		if !ok || w == 0 || h == 0 {
			continue
		}
		cells := len([]rune(key))
		boxW := cells * cellW
		pix, w, h, left, top = fitPrivateUse(pix, w, h, left, top, boxW, cellH)
		candidates = append(candidates, ligCandidate{key, pix, w, h, left, top, cells, boxW})
	}
	if len(candidates) == 0 {
		return nil
	}

	mainBounds := atlas.Image.Bounds()
	targetRowW := max(mainBounds.Dx(), cellW*2)
	slotWidths := make([]int, len(candidates))
	for i, c := range candidates {
		slotWidths[i] = c.slotW
	}
	xs, ys, regionW, regionH := packShelves(slotWidths, glyphPadding, cellH, targetRowW)
	finalW, finalH := max(mainBounds.Dx(), regionW), mainBounds.Dy()+regionH
	if maxTextureSize > 0 && (finalW > maxTextureSize || finalH > maxTextureSize) {
		return fmt.Errorf("atlas texture %dx%d (with ligature glyphs) exceeds this GPU's max texture size (%d) — lower atlas.scale or font.size, or disable font.ligatures",
			finalW, finalH, maxTextureSize)
	}

	grown := image.NewAlpha(image.Rect(0, 0, finalW, finalH))
	draw.Draw(grown, mainBounds, atlas.Image, image.Point{}, draw.Src)
	atlas.Image = grown

	yOffset := mainBounds.Dy()
	for i, c := range candidates {
		gx, gy := xs[i], yOffset+ys[i]
		blitCoverage(c.pix, c.w, c.h, c.left, c.top, atlas.Image, gx, gy, c.slotW, cellH, ascender, gamma)
		atlas.Ligatures[c.key] = Ligature{Glyph: Glyph{X: gx, Y: gy, W: c.slotW, H: cellH}, Cells: c.cells}
	}
	return nil
}

// discoverLigatures searches ligatureAlphabet combinatorially, via
// shaper, for every rune sequence the font collapses into a single
// glyph, and returns them keyed by the literal sequence with the
// resulting glyph ID.
//
// Exhaustively testing every possible sequence up to maxLigatureLen is
// infeasible (94^6), so this grows breadth-first instead: test every
// pair outright (94x94, fast), then only extend sequences that already
// ligated — trying one more alphabet character on either side — up to
// maxLigatureLen. This assumes a real ligature font's longer rules
// build on shorter ones that also independently ligate (true of every
// font this was checked against: FiraCode, JetBrains Mono, Cascadia
// Code — their multi-character triggers are extensions of a shorter
// ligating prefix/suffix, since that mirrors how calt rule chains and
// GSUB ligature-of-ligatures substitutions are actually authored). A
// font with a ligature whose every shorter sub-sequence fails to ligate
// on its own would be missed; no font encountered so far does that.
func discoverLigatures(shaper *hbShaper) map[string]uint32 {
	alphabet := ligatureAlphabet()
	found := make(map[string]uint32)

	var frontier [][]rune
	for _, a := range alphabet {
		for _, b := range alphabet {
			seq := []rune{a, b}
			if gid, ok := shaper.shapeCollapsesToOne(seq); ok {
				found[string(seq)] = gid
				frontier = append(frontier, seq)
			}
		}
	}

	for length := 3; length <= maxLigatureLen && len(frontier) > 0; length++ {
		tried := make(map[string]bool)
		var next [][]rune
		tryGrown := func(grown []rune) {
			key := string(grown)
			if _, already := found[key]; already || tried[key] {
				return
			}
			tried[key] = true
			if gid, ok := shaper.shapeCollapsesToOne(grown); ok {
				found[key] = gid
				next = append(next, grown)
			}
		}
		for _, seq := range frontier {
			for _, c := range alphabet {
				tryGrown(append([]rune{c}, seq...))
				tryGrown(append(append([]rune{}, seq...), c))
			}
		}
		frontier = next
	}
	return found
}
