package font

import (
	"fmt"
	"image"
	"image/draw"
)

// Ligature is one Atlas.Ligatures entry, in one of two shapes a real
// font's GSUB rules can produce for a rune sequence (see discoverLigatures):
//
//   - A true merge into one glyph (PerCell nil): Glyph is that single
//     already-fit-and-packed glyph, and Cells is how many grid columns
//     wide it must be drawn across — the exact number of cells the rune
//     sequence it replaces occupied, unlike WideGlyphs' variable,
//     fit-driven width. CellPass stretches its quad to exactly
//     Cells*cellW rather than deriving a ratio the way it does for
//     WideGlyphs, since a merged ligature must land flush on the grid
//     columns the characters it replaces used to occupy.
//   - A same-count contextual reshape (PerCell non-nil, len == Cells):
//     the font keeps one glyph per character but reshapes each one via
//     a chaining 'calt' rule so adjacent glyphs visually connect —
//     FiraCode, Cascadia Code, and JetBrains Mono all do this rather
//     than merging, since a genuine 1-glyph merge would need the
//     renderer to stretch that merged glyph across N cells the way this
//     package's own Glyph/Cells pair above does, and none of those
//     fonts assume a renderer capable of that. Each entry in PerCell is
//     a normal, single-cell-sized glyph — Glyph/Cells above are unused.
type Ligature struct {
	Glyph
	Cells   int
	PerCell []Glyph
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

// ligCandidate is one still-unpacked merged-glyph Ligature discovery
// result, keyed by the literal rune sequence it replaces — mirrors
// wideIcon's role for buildWideGlyphs.
type ligCandidate struct {
	key       string
	pix       []byte
	w, h      int
	left, top int
	cells     int
	slotW     int // cells*cellW, this candidate's fixed packed width
}

// fitBitmap is one already-fit rasterized glyph bitmap, ready to blit —
// the common shape both ligCandidate's single glyph and
// reshapedCandidate's per-cell glyphs end up in after fitPrivateUse.
type fitBitmap struct {
	pix       []byte
	w, h      int
	left, top int
}

// reshapedCandidate is one still-unpacked contextual-reshape Ligature
// discovery result (see Ligature's own doc comment on the two shapes) —
// one already-fit glyph per matched grid column, each sized to the
// normal single-cell box rather than ligCandidate's Cells*cellW slot.
type reshapedCandidate struct {
	key    string
	glyphs []fitBitmap
}

// buildLigatures discovers and rasterizes every ligature fontBytes's own
// font actually defines (see discoverLigatures — both the merged and the
// contextual-reshape shape), packing them into atlas.Ligatures the same
// way buildWideGlyphs packs Atlas.WideGlyphs — a second region
// shelf-packed onto the bottom of atlas.Image. A no-op (atlas.Ligatures
// left as an empty, non-nil map) whenever the font defines no ligatures
// the search corpus reaches.
func buildLigatures(atlas *Atlas, fontBytes []byte, face *ftFace, cellW, cellH, ascender int, gamma float64, glyphPadding, maxTextureSize int) error {
	atlas.Ligatures = map[string]Ligature{}

	shaper := newHBShaper(fontBytes)
	defer shaper.free()
	rules := discoverLigatures(shaper)
	if len(rules) == 0 {
		return nil
	}

	var merged []ligCandidate
	var reshaped []reshapedCandidate
	for key, rule := range rules {
		if rule.Merged {
			pix, w, h, left, top, ok := face.glyphBitmapByIndex(rule.GID)
			if !ok || w == 0 || h == 0 {
				continue
			}
			cells := len([]rune(key))
			boxW := cells * cellW
			pix, w, h, left, top = fitPrivateUse(pix, w, h, left, top, boxW, cellH)
			merged = append(merged, ligCandidate{key, pix, w, h, left, top, cells, boxW})
			continue
		}

		rc := reshapedCandidate{key: key}
		complete := true
		for _, gid := range rule.GIDs {
			pix, w, h, left, top, ok := face.glyphBitmapByIndex(gid)
			if !ok {
				complete = false
				break
			}
			pix, w, h, left, top = fitPrivateUse(pix, w, h, left, top, cellW, cellH)
			rc.glyphs = append(rc.glyphs, fitBitmap{pix, w, h, left, top})
		}
		if !complete || len(rc.glyphs) != len(rule.GIDs) {
			continue
		}
		reshaped = append(reshaped, rc)
	}
	if len(merged) == 0 && len(reshaped) == 0 {
		return nil
	}

	// One flat shelf-packing pass covers every glyph this function
	// rasterizes: each merged candidate contributes one Cells*cellW
	// slot, each reshaped candidate contributes one cellW slot per
	// matched column — packShelves doesn't care what's being packed,
	// only slotWidths (see its own doc comment). packedSlot records
	// which final Ligature each packed position belongs to, so the
	// blit loop below can write results back after packing decides
	// everyone's (x,y).
	type packedSlot struct {
		reshapedIdx int // -1 for a merged candidate
		glyphIdx    int // merged: index into merged; reshaped: index into reshaped[reshapedIdx].glyphs
	}
	var slotWidths []int
	var slots []packedSlot
	for i, c := range merged {
		slotWidths = append(slotWidths, c.slotW)
		slots = append(slots, packedSlot{reshapedIdx: -1, glyphIdx: i})
	}
	for ri, rc := range reshaped {
		for gi := range rc.glyphs {
			slotWidths = append(slotWidths, cellW)
			slots = append(slots, packedSlot{reshapedIdx: ri, glyphIdx: gi})
		}
	}

	mainBounds := atlas.Image.Bounds()
	targetRowW := max(mainBounds.Dx(), cellW*2)
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

	reshapedGlyphs := make([][]Glyph, len(reshaped))
	for i, rc := range reshaped {
		reshapedGlyphs[i] = make([]Glyph, len(rc.glyphs))
	}
	for i, s := range slots {
		gx, gy := xs[i], yOffset+ys[i]
		if s.reshapedIdx < 0 {
			c := merged[s.glyphIdx]
			blitCoverage(c.pix, c.w, c.h, c.left, c.top, atlas.Image, gx, gy, c.slotW, cellH, ascender, gamma)
			atlas.Ligatures[c.key] = Ligature{Glyph: Glyph{X: gx, Y: gy, W: c.slotW, H: cellH}, Cells: c.cells}
			continue
		}
		fb := reshaped[s.reshapedIdx].glyphs[s.glyphIdx]
		blitCoverage(fb.pix, fb.w, fb.h, fb.left, fb.top, atlas.Image, gx, gy, cellW, cellH, ascender, gamma)
		reshapedGlyphs[s.reshapedIdx][s.glyphIdx] = Glyph{X: gx, Y: gy, W: cellW, H: cellH}
	}
	for i, rc := range reshaped {
		atlas.Ligatures[rc.key] = Ligature{Cells: len(rc.glyphs), PerCell: reshapedGlyphs[i]}
	}
	return nil
}

// ligatureRule is one discoverLigatures result for a candidate rune
// sequence — the font either merges the whole sequence into one glyph
// (Merged, GID set) or keeps one glyph per rune but reshapes each
// contextually so they visually connect (!Merged, GIDs set, one entry
// per rune in sequence order) — see Ligature's own doc comment for why
// both shapes exist and which real fonts use which.
type ligatureRule struct {
	Merged bool
	GID    uint32
	GIDs   []uint32
}

// isolatedGIDs shapes every rune in alphabet entirely on its own and
// records the glyph ID that comes back — the baseline
// discoverLigatures compares a same-count shaped sequence against, since
// a real contextual substitution must change at least one position's
// glyph relative to that rune's own default shape. A rune HarfBuzz
// doesn't return exactly one glyph for on its own (extremely unlikely
// for plain ASCII) is simply absent, and discoverLigatures then treats
// any sequence containing it as unverifiable and skips it.
func isolatedGIDs(shaper *hbShaper) map[rune]uint32 {
	baseline := make(map[rune]uint32)
	for _, r := range ligatureAlphabet() {
		if gid, ok := shaper.shapeCollapsesToOne([]rune{r}); ok {
			baseline[r] = gid
		}
	}
	return baseline
}

// discoverLigatures searches ligatureAlphabet combinatorially, via
// shaper, for every rune sequence the font's GSUB rules turn into
// something other than each rune's own independent glyph — either a
// true merge into a single glyph, or a same-count contextual reshape —
// and returns them keyed by the literal sequence (see ligatureRule).
//
// Exhaustively testing every possible sequence up to maxLigatureLen is
// infeasible (94^6), so this grows breadth-first instead: test every
// pair outright (94x94, fast), then only extend sequences that already
// matched — trying one more alphabet character on either side — up to
// maxLigatureLen. This assumes a real ligature font's longer rules
// build on shorter ones that also independently match (true of every
// font this was checked against: FiraCode, JetBrains Mono, Cascadia
// Code — their multi-character triggers are extensions of a shorter
// matching prefix/suffix, since that mirrors how calt rule chains and
// GSUB ligature-of-ligatures substitutions are actually authored). A
// font with a ligature whose every shorter sub-sequence fails to match
// on its own would be missed; no font encountered so far does that.
func discoverLigatures(shaper *hbShaper) map[string]ligatureRule {
	alphabet := ligatureAlphabet()
	baseline := isolatedGIDs(shaper)
	found := make(map[string]ligatureRule)

	// tryRune checks one candidate sequence against both shapes a
	// ligature rule can take — a merge first (cheap: shapeCollapsesToOne
	// already stops at glyph count 1), then a same-count contextual
	// reshape, which only counts as a real match if at least one
	// position's glyph actually differs from that rune's own isolated
	// shape (otherwise HarfBuzz just shaped each character independently,
	// same as not being a ligature at all).
	tryRune := func(seq []rune) (ligatureRule, bool) {
		if gid, ok := shaper.shapeCollapsesToOne(seq); ok {
			return ligatureRule{Merged: true, GID: gid}, true
		}
		gids, ok := shaper.shapeSequence(seq)
		if !ok {
			return ligatureRule{}, false
		}
		changed := false
		for i, r := range seq {
			base, known := baseline[r]
			if !known {
				return ligatureRule{}, false
			}
			if gids[i] != base {
				changed = true
			}
		}
		if !changed {
			return ligatureRule{}, false
		}
		return ligatureRule{GIDs: gids}, true
	}

	var frontier [][]rune
	for _, a := range alphabet {
		for _, b := range alphabet {
			seq := []rune{a, b}
			if rule, ok := tryRune(seq); ok {
				found[string(seq)] = rule
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
			if rule, ok := tryRune(grown); ok {
				found[key] = rule
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
