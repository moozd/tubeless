package screen

// RowShift is one detected uniform vertical content shift between two
// consecutive published Screens: rows [Top,Bottom] (inclusive) moved by
// Delta rows — positive means content moved up (e.g. a scroll-up or
// paging down through a buffer), negative means content moved down. See
// DetectContentShift.
type RowShift struct {
	Top, Bottom int
	Delta       int
}

// ColShift is RowShift's horizontal mirror: columns [Left,Right]
// (inclusive) moved by Delta columns — positive means content moved
// left, negative means content moved right. See
// DetectHorizontalContentShift.
type ColShift struct {
	Left, Right int
	Delta       int
}

// Tuning constants for the shift-detection engine (see detectShift).
//
// fuzzyRowMaxDiff/fuzzyColMaxDiff cap tier 2's per-line tolerance as an
// ABSOLUTE character count, not a percentage — this was originally a
// fraction (e.g. "85% must match"), but that scales the wrong way with
// line length: a single differing character out of 15 columns is
// already a 93% match, and on an 80+ column terminal one differing
// character alone clears 98%+. The real thing being tolerated — a
// relativenumber/sign-column gutter — is a fixed WIDTH (typically 5-8
// columns) regardless of how wide the terminal is, so the cap needs to
// be fixed too, or two genuinely unrelated lines on a wide terminal that
// happen to differ by only a handful of characters (a numbered log
// line, a counter, a timestamp) would pass "85% similar" easily and
// read as a spurious mid-screen wobble. confidenceThreshold and
// minShiftBandLines separately guard against a coincidental PARTIAL
// match across the whole band.
//
// The column axis uses its own, stricter set: terminal text has far less
// per-column uniqueness than per-row uniqueness (shared indentation,
// runs of spaces, the same handful of characters recurring at the same
// column across otherwise-unrelated lines), so the row-axis thresholds
// let coincidental "shifts" through on that axis — visible as wobble/
// shifting content rather than a real horizontal scroll.
const (
	fuzzyRowMaxDiff     = 8
	confidenceThreshold = 0.80
	minShiftBandLines   = 2

	fuzzyColMaxDiff        = 2
	confidenceColThreshold = 0.94
	minShiftBandColumns    = 20

	// shiftAuxSkip is how many leading columns (for a row hash) or
	// leading rows (for a column hash) the auxiliary "skip" hash below
	// ignores. It exists so candidate-shift SELECTION itself tolerates a
	// fixed-width leading gutter/header that touches every line in the
	// shifted band (nvim's relativenumber column changes on every
	// visible row, for instance) — without it, tier 1's exact-match
	// count can be zero for the true shift AND zero for an unrelated
	// candidate, and the tie-break picks arbitrarily before tier 2 ever
	// gets a chance to run on the right band. The final confidence
	// decision still goes through the width-independent per-line fuzzy
	// check (see fuzzyRowMaxDiff), so this constant only needs to be
	// generous enough to usually cover a real gutter, not exact.
	shiftAuxSkip = 8
)

// DetectContentShift diffs prev and next's Grid content for a uniform
// vertical shift, ignoring Cell.Attr entirely (see rowHash) so a pure
// attribute change (cursorline highlighting, a sign recolor) can never
// register as a difference on its own. maxShift bounds how many rows of
// shift are considered in either direction, bounding the search cost.
// Returns (RowShift{}, false) if prev/next differ in size or no shift
// clears the confidence threshold — callers should render an ordinary
// unanimated snap in that case, exactly like a plain repaint today.
func DetectContentShift(prev, next *Screen, maxShift int) (RowShift, bool) {
	if !sameDims(prev, next) {
		return RowShift{}, false
	}
	n := prev.Rows
	skip := min(shiftAuxSkip, prev.Cols/2)
	beforeHash := make([]uint64, n)
	afterHash := make([]uint64, n)
	auxBeforeHash := make([]uint64, n)
	auxAfterHash := make([]uint64, n)
	for y := range n {
		beforeHash[y] = rowHash(prev.Grid[y])
		afterHash[y] = rowHash(next.Grid[y])
		auxBeforeHash[y] = rowHash(prev.Grid[y][skip:])
		auxAfterHash[y] = rowHash(next.Grid[y][skip:])
	}
	fuzzy := func(afterIdx, beforeIdx int) int {
		return rowMismatches(next.Grid[afterIdx], prev.Grid[beforeIdx])
	}
	lo, hi, delta, ok := detectShift(n, maxShift, beforeHash, afterHash, auxBeforeHash, auxAfterHash, fuzzy, fuzzyRowMaxDiff, confidenceThreshold, minShiftBandLines, blankHash(prev.Cols), blankHash(prev.Cols-skip))
	if !ok {
		return RowShift{}, false
	}
	return RowShift{Top: lo, Bottom: hi, Delta: delta}, true
}

// DetectHorizontalContentShift is DetectContentShift's column-axis
// mirror, for content that shifts left/right (horizontal pagination, a
// scrolling status line) instead of up/down.
func DetectHorizontalContentShift(prev, next *Screen, maxShift int) (ColShift, bool) {
	if !sameDims(prev, next) {
		return ColShift{}, false
	}
	n := prev.Cols
	skip := min(shiftAuxSkip, prev.Rows/2)
	beforeHash := make([]uint64, n)
	afterHash := make([]uint64, n)
	auxBeforeHash := make([]uint64, n)
	auxAfterHash := make([]uint64, n)
	for x := range n {
		beforeHash[x] = colHash(prev.Grid, x)
		afterHash[x] = colHash(next.Grid, x)
		auxBeforeHash[x] = colHash(prev.Grid[skip:], x)
		auxAfterHash[x] = colHash(next.Grid[skip:], x)
	}
	fuzzy := func(afterIdx, beforeIdx int) int {
		return colMismatches(next.Grid, prev.Grid, afterIdx, beforeIdx)
	}
	lo, hi, delta, ok := detectShift(n, maxShift, beforeHash, afterHash, auxBeforeHash, auxAfterHash, fuzzy, fuzzyColMaxDiff, confidenceColThreshold, minShiftBandColumns, blankHash(prev.Rows), blankHash(prev.Rows-skip))
	if !ok {
		return ColShift{}, false
	}
	return ColShift{Left: lo, Right: hi, Delta: delta}, true
}

// sameDims reports whether prev and next are diffable at all: equal,
// non-zero dimensions. Defense-in-depth alongside the caller's own
// same-size gate (cmd/tubeless only calls the detector when the
// published Screen's size hasn't changed) — this makes both exported
// functions safe to call from anywhere without relying on that gate
// being replicated correctly.
func sameDims(prev, next *Screen) bool {
	return prev != nil && next != nil &&
		prev.Cols == next.Cols && prev.Rows == next.Rows &&
		prev.Cols > 0 && prev.Rows > 0
}

// detectShift is the shared engine behind DetectContentShift (rows) and
// DetectHorizontalContentShift (columns). n is the line count on
// whichever axis is being searched (Rows or Cols); beforeHash/afterHash
// are that axis's per-line rune-content hashes for the previous/next
// grid; auxBeforeHash/auxAfterHash are the same, but computed over each
// line with a small fixed leading run skipped (see shiftAuxSkip) so a
// gutter/header that touches every line in the true shifted band still
// registers as a match during candidate selection, even though its
// content technically differs at every line under an exact full-line
// hash; fuzzyDiff reports tier-2's per-line rune-mismatch COUNT (an
// absolute number of differing cells, not a fraction — see
// fuzzyRowMaxDiff/fuzzyColMaxDiff's doc for why) between an after-line
// index and a before-line index.
//
// Tier 1: for each candidate shift k, count how many lines match —
// either their full hash or their aux (skip-prefix) hash exactly equals
// the corresponding before-line's hash at offset k. This is still cheap
// (O(n) per candidate, all hashes precomputed) and, critically, gives a
// real, non-zero signal for the true shift even when a fixed-width
// leading gutter/header would otherwise make every line's full hash
// differ — without the aux hash, a real shift whose entire band carries
// such noise could tie at zero exact matches with an unrelated
// candidate, and the tie-break would pick arbitrarily before tier 2 ever
// gets a chance to look at the right band.
//
// Tier 2: for the winning candidate, lines that missed both hash checks
// are given a second chance via fuzzyDiff, tolerating a narrower or
// differently-shaped difference than the fixed aux-skip width assumes —
// as long as the absolute number of differing cells stays within
// maxDiff (see fuzzyRowMaxDiff/fuzzyColMaxDiff's doc for why this is a
// fixed count, not a percentage).
//
// The matched band is then trimmed to its contiguous core (dropping
// leading/trailing non-matching lines from either edge) so a fixed
// header/footer/gutter sitting at an edge of the candidate band doesn't
// drag the whole thing below the confidence threshold, before a final
// confidence + minimum-size check decides whether to report a shift at
// all. maxDiff/confThreshold/minLines are axis-specific (see
// DetectContentShift/DetectHorizontalContentShift's callers) since the
// column axis needs stricter values than the row axis to avoid
// coincidental matches in low-per-column-entropy text.
//
// blank is rowHash/colHash's value for an all-space line of this axis's
// length (see blankHash) — every blank line hashes identically, so a
// blank-vs-blank pair "matches" at literally any candidate shift k. On a
// terminal that's mostly empty (a shell prompt with one line of real
// text and dozens of blank rows below it, the overwhelmingly common
// case), that trivial agreement can dominate both tier 1's candidate
// score and the final confidence ratio, firing a shift on every
// keystroke even though nothing actually moved. Any pair where both
// sides hash to blank is therefore excluded from counting as evidence
// at every stage below — it neither helps nor hurts, it's just ignored.
// auxBlank is the same idea for the aux (skip-prefix) hash: when the
// only real content on a line falls entirely within the skipped prefix
// (shiftAuxSkip), the aux hash for every such line degenerates to "blank
// after the skip" too, which would otherwise let the aux-match half of
// tier 1/2's "full or aux" check fire everywhere — auxBlank lets that
// specific match be recognized as equally uninformative and rejected,
// without discarding a real full-hash match on the same line.
func detectShift(n, maxShift int, beforeHash, afterHash, auxBeforeHash, auxAfterHash []uint64, fuzzyDiff func(afterIdx, beforeIdx int) int, maxDiff int, confThreshold float64, minLines int, blank, auxBlank uint64) (lo, hi, delta int, ok bool) {
	if maxShift <= 0 || maxShift >= n {
		maxShift = n - 1
	}
	if maxShift <= 0 {
		return 0, 0, 0, false
	}

	bestK, bestExact := 0, -1
	bestLo, bestHi := 0, -1
	for absK := 1; absK <= maxShift; absK++ {
		for _, k := range [2]int{absK, -absK} {
			candLo, candHi := max(0, -k), min(n-1, n-1-k)
			if candLo > candHi {
				continue
			}
			exact := 0
			for y := candLo; y <= candHi; y++ {
				if afterHash[y] == blank && beforeHash[y+k] == blank {
					continue
				}
				auxMatch := auxAfterHash[y] == auxBeforeHash[y+k] &&
					!(auxAfterHash[y] == auxBlank && auxBeforeHash[y+k] == auxBlank)
				if afterHash[y] == beforeHash[y+k] || auxMatch {
					exact++
				}
			}
			if exact > bestExact {
				bestExact, bestK, bestLo, bestHi = exact, k, candLo, candHi
			}
		}
	}
	if bestHi < bestLo {
		return 0, 0, 0, false
	}

	// Tier 2: classify every line in the winning band as matching
	// (tier-1 exact or aux, or tier-2 fuzzy) or not. informative marks a
	// line as carrying real evidence either way — false only for a
	// blank-vs-blank pair, which counts toward neither a match nor a
	// mismatch below (see detectShift's doc).
	matching := make([]bool, bestHi-bestLo+1)
	informative := make([]bool, bestHi-bestLo+1)
	for y := bestLo; y <= bestHi; y++ {
		i := y - bestLo
		if afterHash[y] == blank && beforeHash[y+bestK] == blank {
			matching[i] = true // neutral for trimming — see the total/matchCount loop below for where blank pairs are actually excluded
			continue
		}
		informative[i] = true
		auxMatch := auxAfterHash[y] == auxBeforeHash[y+bestK] &&
			!(auxAfterHash[y] == auxBlank && auxBeforeHash[y+bestK] == auxBlank)
		if afterHash[y] == beforeHash[y+bestK] || auxMatch {
			matching[i] = true
			continue
		}
		matching[i] = fuzzyDiff(y, y+bestK) <= maxDiff
	}

	// Trim leading/trailing non-matching runs off both edges — a fixed
	// header/footer/gutter line sitting at an edge of the band is
	// excluded rather than counted against confidence.
	start, end := 0, len(matching)-1
	for start <= end && !matching[start] {
		start++
	}
	for end >= start && !matching[end] {
		end--
	}
	if start > end {
		return 0, 0, 0, false
	}

	// total/matchCount only count informative lines — a band that's
	// mostly blank padding around one real line of content must be
	// judged on that one line, not on how much blank padding happened to
	// agree (which is guaranteed, not evidence of anything).
	total := 0
	matchCount := 0
	for i := start; i <= end; i++ {
		if !informative[i] {
			continue
		}
		total++
		if matching[i] {
			matchCount++
		}
	}
	if total < minLines {
		return 0, 0, 0, false
	}
	if float64(matchCount)/float64(total) < confThreshold {
		return 0, 0, 0, false
	}

	finalLo, finalHi := bestLo+start, bestLo+end
	// Extend the reported band out to cover brand-new lines the shift
	// itself revealed at an edge — lines with no counterpart in the
	// previous grid at all (never inside the overlap range to begin
	// with), not lines that were in the overlap and simply didn't match
	// (a fixed header/footer, correctly trimmed above). Left un-extended,
	// those brand-new lines render at their normal, unshifted position
	// from the very first frame while the matched band right next to
	// them is still easing in from its offset starting position — the
	// seam between "already settled" and "still animating" reads as a
	// gap that suddenly closes/fills once the glide catches up. Folding
	// the new lines into the same band means they glide in with
	// everything else instead.
	//
	// Known tradeoff: a genuinely static line sitting exactly within the
	// last |shift| lines of this axis (e.g. a status bar pinned to the
	// very bottom row) has no prev counterpart either, for the same
	// reason the real new content doesn't — so it gets swept into the
	// extended band and animates along with everything else, same as a
	// false positive would. This is deliberately accepted: a status
	// bar's own content usually repaints every frame regardless (clock,
	// counters), which already reads as a snap rather than a glide in
	// practice, and the common case (new content actually was revealed)
	// is far more frequent than a single fixed line landing in that
	// exact edge band.
	switch {
	case bestK > 0 && end == len(matching)-1 && bestHi < n-1:
		// Content moved up: new lines appear at the trailing (bottom)
		// edge, past the natural overlap — extend down to the last line,
		// but only if trimming didn't already cut something off this
		// same edge (end reaching the overlap's own end confirms that).
		finalHi = n - 1
	case bestK < 0 && start == 0 && bestLo > 0:
		// Content moved down: new lines appear at the leading (top)
		// edge — extend up to the first line, same reasoning mirrored.
		finalLo = 0
	}

	// A real scroll always originates from (and extends to) one edge of
	// the screen — content enters from the top or bottom (or left/right
	// on the column axis) and everything between that edge and wherever
	// it stops moves together. A band that touches NEITHER edge is a
	// "floating island": unrelated, unshifted content on both sides of
	// it, which a genuine scroll never produces. That shape shows up as
	// a coincidental match — e.g. two otherwise-static lines elsewhere
	// on a full screen happening to resemble each other under some small
	// k while the user types on a completely different line — and was
	// observed as a spurious wobble in the middle of an otherwise-still
	// screen. Reject it rather than animate a shift nothing real caused.
	if finalLo > 0 && finalHi < n-1 {
		return 0, 0, 0, false
	}
	return finalLo, finalHi, bestK, true
}

// blankHash is the hash rowHash/colHash produce for a line of n cells
// that are all Cell{Rune: ' '} — a real terminal's blank-cell fill (see
// newRow/blankCell). Comparing against this lets detectShift recognize
// an all-blank line cheaply, from an already-computed hash, without
// rescanning its actual content.
func blankHash(n int) uint64 {
	h := uint64(14695981039346656037)
	for range n {
		h = foldRune(h, ' ')
	}
	return h
}

// rowHash is an FNV-1a hash over a row's rune content only — Cell.Attr
// is never consulted, so a pure attribute change (highlighting, color)
// can never register as a content difference.
func rowHash(row []Cell) uint64 {
	h := uint64(14695981039346656037)
	for _, c := range row {
		h = foldRune(h, c.Rune)
	}
	return h
}

// colHash is rowHash's transpose: a hash over one column's rune content
// down every row of grid.
func colHash(grid [][]Cell, col int) uint64 {
	h := uint64(14695981039346656037)
	for _, row := range grid {
		h = foldRune(h, row[col].Rune)
	}
	return h
}

// foldRune folds one rune's 4 bytes into an FNV-1a accumulator, avoiding
// a per-rune/per-row []byte allocation or hash.Hash64 interface dispatch
// on what can run for every row and column of every changed frame.
func foldRune(h uint64, r rune) uint64 {
	const prime64 = 1099511628211
	v := uint32(r)
	h = (h ^ uint64(v&0xff)) * prime64
	h = (h ^ uint64((v>>8)&0xff)) * prime64
	h = (h ^ uint64((v>>16)&0xff)) * prime64
	h = (h ^ uint64((v>>24)&0xff)) * prime64
	return h
}

// rowMismatches is tier 2's per-row check: how many positions a's runes
// differ from b's runes at the same column index — an absolute count,
// not a fraction (see fuzzyRowMaxDiff's doc for why). A position where
// BOTH sides are blank (space) is skipped entirely rather than counted
// as a match: most shell commands are short, so a row is mostly blank
// padding out to the terminal's full width — without this, two
// completely unrelated short commands (e.g. "cd .." vs "ls -la") would
// share almost all of that padding and read as differing by only a
// handful of characters, the same trivial-agreement problem the
// whole-row blank check (see detectShift's blank parameter) solves one
// level up, just recurring at the per-cell level within a single row
// that isn't blank overall. A length mismatch (shouldn't happen — both
// rows share the screen's Cols) counts as maximally different, never a
// coincidental pass.
func rowMismatches(a, b []Cell) int {
	n := len(a)
	if n == 0 || len(b) != n {
		return n + 1
	}
	diff := 0
	for i := range n {
		if a[i].Rune == ' ' && b[i].Rune == ' ' {
			continue
		}
		if a[i].Rune != b[i].Rune {
			diff++
		}
	}
	return diff
}

// colMismatches is rowMismatches' transpose: how many rows column colA
// in gridA differs from column colB in gridB at the same row index.
func colMismatches(gridA, gridB [][]Cell, colA, colB int) int {
	n := len(gridA)
	if n == 0 || len(gridB) != n {
		return n + 1
	}
	diff := 0
	for y := range n {
		if gridA[y][colA].Rune == ' ' && gridB[y][colB].Rune == ' ' {
			continue
		}
		if gridA[y][colA].Rune != gridB[y][colB].Rune {
			diff++
		}
	}
	return diff
}
