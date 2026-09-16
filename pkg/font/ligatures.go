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
//
// A real font's GSUB rules produce this in one of two ways (see
// discoverLigatures/buildLigatures), both ending up as this same single
// packed glyph: a true merge, where the font itself collapses the whole
// rune sequence into one designed glyph, or a same-count contextual
// reshape, where the font keeps one glyph per character but reshapes
// each one via a chaining 'calt' rule so adjacent glyphs visually
// connect — how FiraCode, Cascadia Code, and JetBrains Mono actually
// implement their arrow/comparison ligatures. buildLigatures composites
// a reshape's several glyphs onto one shared Cells*cellW canvas at their
// natural per-character offsets before packing, so CellPass never needs
// to know which shape produced the result it's drawing.
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

// ligCandidate is one still-unpacked merged-glyph Ligature discovery
// result, keyed by the literal rune sequence it replaces — mirrors
// wideIcon's role for buildWideGlyphs. A merge (glyphs has exactly one
// entry, already fit to the whole slotW via fitPrivateUse — the font
// drew this one specifically to span Cells columns, same as an icon
// spans its own box) blits as that single bitmap; a contextual reshape
// (glyphs has one raw, unfit entry per matched column) blits each at
// its own nominal i*cellW offset within the shared slotW box instead —
// deliberately raw rather than individually fit, since a real
// connecting reshape depends on each glyph's own FreeType bearings
// landing where the font actually drew them relative to its neighbors.
// FiraCode's "!=" is the concrete case this exists for: shaping it
// yields a first glyph that's entirely blank and a second one whose
// left bearing is negative — bleeding leftward into the first,
// now-empty column — so the two only read as a connected "≠" once
// blitted into one box wide enough to receive that overflow;
// fitPrivateUse's fit-to-box rescaling would discard the bearing that
// makes the connection visible in the first place.
type ligCandidate struct {
	key    string
	cells  int
	slotW  int // cells*cellW, this candidate's fixed packed width
	glyphs []fitBitmap
}

// fitBitmap is one rasterized glyph bitmap, ready to blit at some
// caller-chosen (gx, gy) — see ligCandidate's own doc comment for the
// two ways a candidate's glyphs get here (fit vs. raw).
type fitBitmap struct {
	pix       []byte
	w, h      int
	left, top int
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

	var candidates []ligCandidate
	for key, rule := range rules {
		cells := len([]rune(key))
		boxW := cells * cellW
		if rule.Merged {
			pix, w, h, left, top, ok := face.glyphBitmapByIndex(rule.GID)
			if !ok || w == 0 || h == 0 {
				continue
			}
			pix, w, h, left, top = fitPrivateUse(pix, w, h, left, top, boxW, cellH)
			candidates = append(candidates, ligCandidate{key, cells, boxW, []fitBitmap{{pix, w, h, left, top}}})
			continue
		}

		// A reshape: rasterize each position's own glyph raw (no
		// fitPrivateUse — see ligCandidate's doc comment), keeping a
		// blank one (a glyph that legitimately has zero ink, like the
		// first half of FiraCode's "!=" — see isReshapeCandidate's
		// caller) rather than rejecting the whole candidate over it.
		// Only a genuinely unresolvable glyph ID does that.
		glyphs := make([]fitBitmap, len(rule.GIDs))
		ok := true
		for i, gid := range rule.GIDs {
			pix, w, h, left, top, valid := face.glyphBitmapByIndex(gid)
			if !valid {
				ok = false
				break
			}
			if w == 0 || h == 0 {
				continue // blank glyph at this position — nothing to blit, not an error
			}
			if w > cellW || h > cellH {
				// Mirrors blitGlyph's own overflow guard for a normal
				// text glyph: an independent per-axis clamp, not a
				// uniform fitPrivateUse-style scale, so a width-only
				// overflow doesn't also shrink height.
				ow, oh := min(w, cellW), min(h, cellH)
				wScale, hScale := float64(ow)/float64(w), float64(oh)/float64(h)
				pix, w, h = resizeCoverage(pix, w, h, ow, oh)
				left, top = int(float64(left)*wScale), int(float64(top)*hScale)
			}
			glyphs[i] = fitBitmap{pix, w, h, left, top}
		}
		if !ok {
			continue
		}
		candidates = append(candidates, ligCandidate{key, cells, boxW, glyphs})
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
		// A merge's one glyph already fills slotW; a reshape's several
		// each land at their own nominal cell offset within it, so
		// blitCoverage's clamp only ever kicks in at the whole box's
		// outer edges — never between two of a reshape's own columns —
		// letting a negative left bearing bleed into its neighbor
		// exactly as the font intends (see ligCandidate's doc comment).
		for j, g := range c.glyphs {
			if g.w == 0 || g.h == 0 {
				continue
			}
			blitCoverage(g.pix, g.w, g.h, g.left, g.top, atlas.Image, gx+j*cellW, gy, c.slotW, cellH, ascender, gamma)
		}
		atlas.Ligatures[c.key] = Ligature{Glyph: Glyph{X: gx, Y: gy, W: c.slotW, H: cellH}, Cells: c.cells}
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

// isReshapeCandidate reports whether r belongs to the character set a
// real contextual-reshape programming ligature is ever built from:
// punctuation, never a letter or digit — see tryRune's own doc comment
// for why the reshape check (unlike the merge check) restricts to this
// narrower set.
func isReshapeCandidate(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return false
	default:
		return true
	}
}

// maxLigatureTries hard-bounds discoverLigatures' total shaping work
// regardless of what the font actually defines. The initial pair pass
// alone (94x94, unconditional) is always cheap; the risk is the
// growth phase after it (see discoverLigatures' own doc comment),
// which is bounded above by frontier-size * alphabet-size * 2 per
// round — fine for a collapse-style font (a real single-glyph merge is
// rare, so the frontier stays in the dozens-to-low-hundreds) but not
// for a contextual-reshape font with rich 'calt' rules: FiraCode's
// chaining rules leave a large fraction of ALL punctuation pairs
// reshaped relative to their isolated glyph (see shapeSequence's own
// doc comment), so the reshape frontier can start in the thousands —
// observed in practice to still be growing, and still using real CPU,
// past 30+ seconds against the genuine upstream FiraCode release.
// Stopping once this many candidate sequences have been shaped keeps
// atlas rebuild time bounded on every font: every 2-character rule is
// still found in full (the pair pass runs unconditionally, before this
// budget is ever checked), only the rarer 3+-character extensions on
// an unusually rule-dense font are the ones that can be cut short.
const maxLigatureTries = 20_000

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
// maxLigatureLen, within maxLigatureTries' hard budget. This assumes a
// real ligature font's longer rules build on shorter ones that also
// independently match (true of every font this was checked against:
// FiraCode, JetBrains Mono, Cascadia Code — their multi-character
// triggers are extensions of a shorter matching prefix/suffix, since
// that mirrors how calt rule chains and GSUB ligature-of-ligatures
// substitutions are actually authored). A font with a ligature whose
// every shorter sub-sequence fails to match on its own would be
// missed; no font encountered so far does that.
func discoverLigatures(shaper *hbShaper) map[string]ligatureRule {
	alphabet := ligatureAlphabet()
	baseline := isolatedGIDs(shaper)
	found := make(map[string]ligatureRule)
	tries := 0

	// tryRune checks one candidate sequence against both shapes a
	// ligature rule can take — a merge first (cheap: shapeCollapsesToOne
	// already stops at glyph count 1, and tried against any candidate,
	// letters included, so a real text ligature like "ffi" is still
	// found the way it always was), then a same-count contextual
	// reshape. The reshape check is deliberately much stricter, since
	// unlike a merge (rare, and always visually obvious when it fires)
	// a contextual glyph swap is common in real fonts for reasons that
	// have nothing to do with a "connecting" ligature — kerning-class
	// substitutions, stylistic variants, letter/digit disambiguation —
	// and an early version of this check that accepted any candidate
	// where at least one position's glyph merely differed from its
	// isolated shape found thousands of such false positives on real
	// FiraCode (letters mixed with punctuation, one changed glyph out
	// of three), most of them meaningless. Two things bring that back
	// down to real programming ligatures specifically: every rune in
	// the sequence must be punctuation (isReshapeCandidate — a letter
	// or digit taking on a contextual variant is never what "ligature"
	// means here), and every position's glyph — not just one — must
	// differ from its own isolated shape, since a real connecting
	// ligature reshapes both ends of the join, not one side in
	// isolation (confirmed against FiraCode's own "<-": both the '<'
	// and the '-' glyph IDs change, never just one).
	tryRune := func(seq []rune) (ligatureRule, bool) {
		tries++
		if gid, ok := shaper.shapeCollapsesToOne(seq); ok {
			return ligatureRule{Merged: true, GID: gid}, true
		}
		for _, r := range seq {
			if !isReshapeCandidate(r) {
				return ligatureRule{}, false
			}
		}
		gids, ok := shaper.shapeSequence(seq)
		if !ok {
			return ligatureRule{}, false
		}
		for i, r := range seq {
			base, known := baseline[r]
			if !known || gids[i] == base {
				return ligatureRule{}, false
			}
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

	for length := 3; length <= maxLigatureLen && len(frontier) > 0 && tries < maxLigatureTries; length++ {
		tried := make(map[string]bool)
		var next [][]rune
		tryGrown := func(grown []rune) {
			if tries >= maxLigatureTries {
				return
			}
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
				if tries >= maxLigatureTries {
					break
				}
				tryGrown(append([]rune{c}, seq...))
				tryGrown(append(append([]rune{}, seq...), c))
			}
		}
		frontier = next
	}
	return found
}
