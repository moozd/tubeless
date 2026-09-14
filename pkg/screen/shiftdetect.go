package screen

// RowShift is one detected uniform vertical content shift between two
// consecutive published Screens: rows [Top,Bottom] (inclusive), within
// columns [Left,Right] (inclusive), moved by Delta rows — positive means
// content moved up (e.g. a scroll-up or paging down through a buffer),
// negative means content moved down. See DetectContentShift. Left/Right
// span the full screen width (0, Cols-1) for an ordinary whole-screen
// shift; a narrower range means the shift was confined to one column
// band — a split window's pane — found by columnBands.
type RowShift struct {
	Top, Bottom int
	Left, Right int
	Delta       int
}

// ColShift is RowShift's horizontal mirror: columns [Left,Right]
// (inclusive), within rows [Top,Bottom] (inclusive), moved by Delta
// columns — positive means content moved left, negative means content
// moved right. See DetectHorizontalContentShift. Top/Bottom span the
// full screen height for an ordinary whole-screen shift; a narrower
// range means the shift was confined to one row band — a horizontally
// split window's pane — found by rowBands.
type ColShift struct {
	Left, Right int
	Top, Bottom int
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

	// minTier1Ratio floors, on both axes, how much of a winning band's
	// evidence must come from tier 1 (an exact or gutter-aux hash match)
	// rather than tier 2's fuzzy fallback alone — see its check in
	// detectShift for why an unfloored tier 2 false-fires on columnar
	// output (ls -la, ps, git log --oneline) where unrelated rows
	// sharing a fixed layout differ by only a handful of characters
	// almost regardless of maxDiff. 0.5 keeps tier 2 a minority-case
	// rescue: at least half the band's real evidence has to be genuine
	// content identity, not "close enough" tolerance.
	minTier1Ratio = 0.5

	// popularityCap bounds how many times a line's hash may recur across
	// the axis before any comparison involving it is treated as
	// uninformative — see its use (as isDegenerate) in detectShift. 3
	// keeps a coincidental duplicate or two (two unrelated lines/columns
	// that just happen to share content) from being excluded, while
	// still catching an actual repeated run, which in practice is never
	// just 2-3 wide.
	popularityCap = 3

	// unshiftedMaxChangedSpan caps unshiftedMatch's affixGap check — see
	// its use there. Not fuzzyRowMaxDiff/fuzzyColMaxDiff: those are tuned
	// for tier 2's job (does an already-known-shifted line also tolerate
	// a fixed-width gutter), a different question from "is this
	// basically the SAME line, just a counter ticking over somewhere in
	// it." 20 comfortably covers a realistic position/percentage
	// indicator changing shape (e.g. "39,0-1  0%" -> "40,1  1%") while
	// callers additionally cap it at half the line's own width (see
	// detectContentShiftInBand/detectHorizontalContentShiftInBand), so a
	// narrow band can't have its entire width trivially pass this check.
	unshiftedMaxChangedSpan = 20

	// anchorlessLineFactor scales minLines (axis-specific: minShiftBandLines
	// or minShiftBandColumns) up to the total a band must clear to be
	// trusted WITHOUT touching a true axis edge — see its use
	// (chromeAnchored) at the end of detectShift. minLines alone is
	// tuned for the edge-touching case, where "a real scroll happened"
	// is already established by the shape alone; a chrome-bounded band
	// carries no such structural guarantee, so it needs to win on
	// evidence volume instead — scaling by the axis's own minLines
	// (rather than one shared constant) keeps the column axis's already
	// much higher bar proportionally higher here too.
	anchorlessLineFactor = 4

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

	// minShiftBandWidth/minShiftBandHeight floor how narrow/short a
	// column/row band (see columnBands/rowBands) can be before it's
	// still worth searching independently — a sliver a divider-detection
	// false-positive carved off isn't wide/tall enough to carry
	// meaningful evidence either way, so skip it rather than let it
	// occasionally squeak past minShiftBandLines/minShiftBandColumns on
	// its own.
	minShiftBandWidth  = 10
	minShiftBandHeight = 4

	// dividerRowRatio is how much of a column's (or row's) height (width)
	// must hold the same non-blank character, in the same position in
	// both prev and next, to count as a persistent divider — see
	// columnBands/rowBands. Not 1.0: a per-pane statusline row and the
	// bottom command-line row are typically full-width and interrupt a
	// vertical divider for exactly those rows, so requiring literal
	// unanimity would mean the divider — and therefore the pane
	// boundary — is never found at all.
	dividerRowRatio = 0.7
)

// verticalDividerRunes/horizontalDividerRunes are the box-drawing
// characters columnBands/rowBands accept as a plausible window-border
// glyph — nvim's default vertical/horizontal split separator, tmux's
// pane border, and their common stylistic variants — kept as separate
// sets since a real vertical divider is always drawn with a vertical
// line and a real horizontal one with a horizontal line; there's no
// reason to let either mistake the other's glyph for a match.
// Restricting to a known glyph set at all (rather than accepting ANY
// constant, unchanged rune) matters: ordinary content regularly has
// long constant runs too — a template's fixed boilerplate text, a block
// of consistently-indented code, a column of repeated punctuation — and
// treating every one of those as a plausible pane boundary fragmented
// the screen into a mess of spurious single-column slivers instead of
// finding the one real divider.
var verticalDividerRunes = map[rune]bool{
	'│': true, '┃': true, '|': true, '║': true,
	'┆': true, '┊': true, '╎': true, '╏': true,
}

var horizontalDividerRunes = map[rune]bool{
	'─': true, '━': true, '═': true,
	'┄': true, '┈': true, '┅': true, '┉': true,
}

// DetectContentShift diffs prev and next's Grid content for a uniform
// vertical shift, ignoring Cell.Attr entirely (see rowHash) so a pure
// attribute change (cursorline highlighting, a sign recolor) can never
// register as a difference on its own. maxShift bounds how many rows of
// shift are considered in either direction, bounding the search cost.
// Returns (RowShift{}, false) if prev/next differ in size or no shift
// clears the confidence threshold — callers should render an ordinary
// unanimated snap in that case, exactly like a plain repaint today.
//
// Tries the whole screen width first; if that finds nothing, falls back
// to searching within each column band columnBands finds (a split
// window's pane) independently — a pane's own scroll never explains the
// physical rows it shares with a frozen neighboring pane, so a
// whole-row comparison alone misses it entirely; see columnBands' doc.
// Only the first band that clears the threshold is reported — two panes
// scrolling independently in the same frame isn't (yet) handled, since
// the renderer only tracks one glide band per axis at a time.
func DetectContentShift(prev, next *Screen, maxShift int) (RowShift, bool) {
	if !sameDims(prev, next) {
		return RowShift{}, false
	}
	if shift, ok := detectContentShiftInBand(prev, next, maxShift, 0, prev.Cols-1); ok {
		return shift, true
	}
	for _, band := range columnBands(prev, next) {
		if band[1]-band[0]+1 < minShiftBandWidth {
			continue
		}
		if shift, ok := detectContentShiftInBand(prev, next, maxShift, band[0], band[1]); ok {
			return shift, true
		}
	}
	return RowShift{}, false
}

// detectContentShiftInBand is DetectContentShift's engine, restricted to
// columns [colLo,colHi] (inclusive) — DetectContentShift itself is just
// this called once over the full width.
func detectContentShiftInBand(prev, next *Screen, maxShift, colLo, colHi int) (RowShift, bool) {
	n := prev.Rows
	width := colHi - colLo + 1
	skip := colLo + min(shiftAuxSkip, width/2)
	beforeHash := make([]uint64, n)
	afterHash := make([]uint64, n)
	auxBeforeHash := make([]uint64, n)
	auxAfterHash := make([]uint64, n)
	for y := range n {
		beforeHash[y] = rowHash(prev.Grid[y][colLo : colHi+1])
		afterHash[y] = rowHash(next.Grid[y][colLo : colHi+1])
		auxBeforeHash[y] = rowHash(prev.Grid[y][skip : colHi+1])
		auxAfterHash[y] = rowHash(next.Grid[y][skip : colHi+1])
	}
	fuzzy := func(afterIdx, beforeIdx int) int {
		return rowMismatches(next.Grid[afterIdx][colLo:colHi+1], prev.Grid[beforeIdx][colLo:colHi+1])
	}
	affixGap := func(afterIdx, beforeIdx int) int {
		return cellAffixGap(next.Grid[afterIdx][colLo:colHi+1], prev.Grid[beforeIdx][colLo:colHi+1])
	}
	lo, hi, delta, ok := detectShift(n, maxShift, beforeHash, afterHash, auxBeforeHash, auxAfterHash, fuzzy, affixGap, fuzzyRowMaxDiff, min(unshiftedMaxChangedSpan, width/2), confidenceThreshold, minShiftBandLines, blankHash(width), blankHash(colHi+1-skip))
	if !ok {
		return RowShift{}, false
	}
	return RowShift{Top: lo, Bottom: hi, Left: colLo, Right: colHi, Delta: delta}, true
}

// DetectHorizontalContentShift is DetectContentShift's column-axis
// mirror, for content that shifts left/right (horizontal pagination, a
// scrolling status line) instead of up/down. Tries the whole screen
// height first, then falls back to each row band rowBands finds (a
// horizontally split window's pane) — see DetectContentShift's doc for
// why, mirrored onto rows instead of columns.
func DetectHorizontalContentShift(prev, next *Screen, maxShift int) (ColShift, bool) {
	if !sameDims(prev, next) {
		return ColShift{}, false
	}
	if shift, ok := detectHorizontalContentShiftInBand(prev, next, maxShift, 0, prev.Rows-1); ok {
		return shift, true
	}
	for _, band := range rowBands(prev, next) {
		if band[1]-band[0]+1 < minShiftBandHeight {
			continue
		}
		if shift, ok := detectHorizontalContentShiftInBand(prev, next, maxShift, band[0], band[1]); ok {
			return shift, true
		}
	}
	return ColShift{}, false
}

// detectHorizontalContentShiftInBand is DetectHorizontalContentShift's
// engine, restricted to rows [rowLo,rowHi] (inclusive).
func detectHorizontalContentShiftInBand(prev, next *Screen, maxShift, rowLo, rowHi int) (ColShift, bool) {
	n := prev.Cols
	height := rowHi - rowLo + 1
	skip := rowLo + min(shiftAuxSkip, height/2)
	beforeHash := make([]uint64, n)
	afterHash := make([]uint64, n)
	auxBeforeHash := make([]uint64, n)
	auxAfterHash := make([]uint64, n)
	for x := range n {
		beforeHash[x] = colHash(prev.Grid[rowLo:rowHi+1], x)
		afterHash[x] = colHash(next.Grid[rowLo:rowHi+1], x)
		auxBeforeHash[x] = colHash(prev.Grid[skip:rowHi+1], x)
		auxAfterHash[x] = colHash(next.Grid[skip:rowHi+1], x)
	}
	fuzzy := func(afterIdx, beforeIdx int) int {
		return colMismatches(next.Grid[rowLo:rowHi+1], prev.Grid[rowLo:rowHi+1], afterIdx, beforeIdx)
	}
	affixGap := func(afterIdx, beforeIdx int) int {
		return colAffixGap(next.Grid[rowLo:rowHi+1], prev.Grid[rowLo:rowHi+1], afterIdx, beforeIdx)
	}
	lo, hi, delta, ok := detectShift(n, maxShift, beforeHash, afterHash, auxBeforeHash, auxAfterHash, fuzzy, affixGap, fuzzyColMaxDiff, min(unshiftedMaxChangedSpan, height/2), confidenceColThreshold, minShiftBandColumns, blankHash(height), blankHash(rowHi+1-skip))
	if !ok {
		return ColShift{}, false
	}
	return ColShift{Left: lo, Right: hi, Top: rowLo, Bottom: rowHi, Delta: delta}, true
}

// columnBands splits the screen width into the column ranges between
// persistent vertical divider columns — nvim's default vertical-split
// separator, a tmux pane border: a column drawing the same non-blank
// character down (most of) the screen's height, in the SAME position in
// both prev and next, since a real divider doesn't move. Doesn't require
// literally every row to agree — a per-pane statusline row and the
// bottom command-line row are typically full-width and interrupt the
// divider, so dividerRowRatio only requires a strong majority.
//
// This only finds a divider by its rune; a divider drawn as a plain
// colored blank column (no distinguishing character — rare, but some
// themes do this) is indistinguishable from ordinary blank content and
// won't be found, so a pane scroll behind one still won't animate. That
// matches today's (no detection at all) behavior for that case, not a
// regression.
func columnBands(prev, next *Screen) [][2]int {
	cols, rows := prev.Cols, prev.Rows
	isDivider := func(c int) bool {
		matches := 0
		for y := range rows {
			r := prev.Grid[y][c].Rune
			if verticalDividerRunes[r] && next.Grid[y][c].Rune == r {
				matches++
			}
		}
		return float64(matches)/float64(rows) >= dividerRowRatio
	}
	var bands [][2]int
	start := 0
	for c := range cols {
		if !isDivider(c) {
			continue
		}
		if c > start {
			bands = append(bands, [2]int{start, c - 1})
		}
		start = c + 1
	}
	if start < cols {
		bands = append(bands, [2]int{start, cols - 1})
	}
	return bands
}

// rowBands is columnBands' transpose: the row ranges between persistent
// horizontal divider rows (a horizontally split window's border). See
// columnBands' doc for the matching rules and its known blank-divider
// gap.
func rowBands(prev, next *Screen) [][2]int {
	cols, rows := prev.Cols, prev.Rows
	isDivider := func(y int) bool {
		matches := 0
		for x := range cols {
			r := prev.Grid[y][x].Rune
			if horizontalDividerRunes[r] && next.Grid[y][x].Rune == r {
				matches++
			}
		}
		return float64(matches)/float64(cols) >= dividerRowRatio
	}
	var bands [][2]int
	start := 0
	for y := range rows {
		if !isDivider(y) {
			continue
		}
		if y > start {
			bands = append(bands, [2]int{start, y - 1})
		}
		start = y + 1
	}
	if start < rows {
		bands = append(bands, [2]int{start, rows - 1})
	}
	return bands
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
func detectShift(n, maxShift int, beforeHash, afterHash, auxBeforeHash, auxAfterHash []uint64, fuzzyDiff func(afterIdx, beforeIdx int) int, affixGap func(afterIdx, beforeIdx int) int, maxDiff int, maxChangedSpan int, confThreshold float64, minLines int, blank, auxBlank uint64) (lo, hi, delta int, ok bool) {
	if maxShift <= 0 || maxShift >= n {
		maxShift = n - 1
	}
	if maxShift <= 0 {
		return 0, 0, 0, false
	}

	// popular counts how many times each full-line hash recurs across
	// this axis, on each side independently. A repeated-character run —
	// a themed prompt's horizontal rule, a tmux status bar's separator,
	// a `yes`-style burst of identical rows — makes many lines' hashes
	// genuinely, exactly IDENTICAL, not merely similar: any shift within
	// that run trivially satisfies tier 1, no matter how tight
	// maxDiff/confThreshold are, because the individual comparison isn't
	// wrong — the content really is ambiguous about which occurrence
	// corresponds to which. popularityCap-and-above hash values are
	// therefore excluded from counting as evidence below (see
	// isDegenerate), the same treatment already given to blank.
	popular := func(hashes []uint64) map[uint64]int {
		counts := make(map[uint64]int, n)
		for _, h := range hashes {
			counts[h]++
		}
		return counts
	}
	beforeCount, afterCount := popular(beforeHash), popular(afterHash)
	isDegenerate := func(afterIdx, beforeIdx int) bool {
		return beforeCount[beforeHash[beforeIdx]] > popularityCap || afterCount[afterHash[afterIdx]] > popularityCap
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
				if isDegenerate(y, y+k) {
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
	// line as carrying real evidence either way — false for a
	// blank-vs-blank pair or a degenerate (isDegenerate) one, neither of
	// which counts toward a match or a mismatch below (see detectShift's
	// doc). tier1 marks a match as coming from an exact/aux hash rather
	// than tier 2's fuzzy fallback — see the tier1Count check below for
	// why this is tracked separately.
	matching := make([]bool, bestHi-bestLo+1)
	informative := make([]bool, bestHi-bestLo+1)
	tier1 := make([]bool, bestHi-bestLo+1)
	for y := bestLo; y <= bestHi; y++ {
		i := y - bestLo
		if afterHash[y] == blank && beforeHash[y+bestK] == blank {
			matching[i] = true // neutral for trimming — see the total/matchCount loop below for where blank pairs are actually excluded
			continue
		}
		if isDegenerate(y, y+bestK) {
			matching[i] = true // neutral for trimming, same as a blank pair — see isDegenerate's doc above
			continue
		}
		informative[i] = true
		auxMatch := auxAfterHash[y] == auxBeforeHash[y+bestK] &&
			!(auxAfterHash[y] == auxBlank && auxBeforeHash[y+bestK] == auxBlank)
		if afterHash[y] == beforeHash[y+bestK] || auxMatch {
			matching[i] = true
			tier1[i] = true
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
	tier1Count := 0
	for i := start; i <= end; i++ {
		if !informative[i] {
			continue
		}
		total++
		if matching[i] {
			matchCount++
		}
		if tier1[i] {
			tier1Count++
		}
	}
	if total < minLines {
		return 0, 0, 0, false
	}
	if float64(matchCount)/float64(total) < confThreshold {
		return 0, 0, 0, false
	}
	// tier 2's fuzzy fallback exists to rescue a handful of lines a fixed
	// leading gutter/header pushes past the aux hash's skip width within
	// an otherwise genuinely-matching band — it was never meant to be the
	// SOLE evidence for a shift. Columnar/tabular output (ls -la, ps,
	// git log --oneline, df) is the adversarial case: consecutive
	// UNRELATED rows sharing the same fixed layout typically differ by
	// only a handful of characters (a date, a short name) regardless of
	// how wide or long the lines are, which clears any fixed absolute
	// maxDiff cap almost every time — so a band with no tier-1
	// corroboration at all can rack up high "confidence" purely from
	// coincidental structural similarity between rows that never moved.
	// Requiring at least half the evidence to be tier-1 keeps tier 2 as
	// the minority-case rescue it was designed to be.
	if float64(tier1Count)/float64(total) < minTier1Ratio {
		return 0, 0, 0, false
	}

	finalLo, finalHi := bestLo+start, bestLo+end
	// unshiftedMatch reports whether line y's content in next basically
	// matches its own content in prev AT THE SAME INDEX (k=0, not the
	// candidate shift) — the signature of a fixed line that doesn't
	// scroll at all (a status bar, a command line) rather than content
	// the shift genuinely revealed. A real newly-revealed line has no
	// meaningful relationship to whatever prev happened to show at that
	// same row/column index, so this is false for it; a status bar
	// showing the same layout with only a cursor-position counter
	// ticking over is barely different from itself frame to frame, so
	// this is true for it. See its use in the extension switch below.
	//
	// The final fallback checks affixGap — the width of content BETWEEN
	// the longest common prefix and longest common suffix — rather than
	// a raw mismatch count (an earlier version of this check did exactly
	// that, and a real status bar's position/percentage indicator turned
	// out to routinely change by MORE characters than a piece of
	// genuinely new, unrelated content sometimes does: "39,0-1  0%" ->
	// "40,1  1%" is 6 raw mismatches, while a real "this row is now
	// something else entirely" case in this file's own tests is only 7
	// — no fixed count cleanly separates them). A real status bar keeps
	// its surrounding layout (filename, padding) byte-identical and only
	// changes a narrow span somewhere in the middle or at one edge;
	// genuinely unrelated content typically shares no meaningful prefix
	// OR suffix with whatever used to be at that row/column at all. That
	// structural difference — not the raw size of the change — is what
	// actually distinguishes them.
	unshiftedMatch := func(y int) bool {
		if afterHash[y] == beforeHash[y] {
			return true
		}
		if afterHash[y] == blank && beforeHash[y] == blank {
			return true
		}
		auxMatch := auxAfterHash[y] == auxBeforeHash[y] &&
			!(auxAfterHash[y] == auxBlank && auxBeforeHash[y] == auxBlank)
		if auxMatch {
			return true
		}
		return affixGap(y, y) <= maxChangedSpan
	}
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
	// Stops extending at the first line that passes unshiftedMatch: a
	// genuinely static line sitting exactly within the last |shift|
	// lines of this axis (a status bar pinned to the very bottom row, a
	// command line) has no prev counterpart under the shift either, for
	// the same reason real new content doesn't, but its own content
	// barely changes frame to frame — extending blindly to the axis edge
	// here (an earlier version of this code did exactly that) swept
	// status bars into the glide, visibly sliding and snapping back on
	// every scroll. Checking unshiftedMatch first tells the two cases
	// apart.
	switch {
	case bestK > 0 && end == len(matching)-1 && bestHi < n-1:
		// Content moved up: new lines appear at the trailing (bottom)
		// edge, past the natural overlap — extend down as long as each
		// next line is genuinely new, but only if trimming didn't
		// already cut something off this same edge (end reaching the
		// overlap's own end confirms that).
		for finalHi+1 <= n-1 && !unshiftedMatch(finalHi+1) {
			finalHi++
		}
	case bestK < 0 && start == 0 && bestLo > 0:
		// Content moved down: new lines appear at the leading (top)
		// edge — extend up the same way, mirrored.
		for finalLo-1 >= 0 && !unshiftedMatch(finalLo-1) {
			finalLo--
		}
	}

	// A real scroll always originates from (and extends to) one edge of
	// the axis — content enters from the top or bottom (or left/right on
	// the column axis) and everything between that edge and wherever it
	// stops moves together. A band touching NEITHER the true axis edge
	// NOR a confirmed-fixed neighbor on both sides is a "floating
	// island": unrelated, unshifted content on both sides of it, which a
	// genuine scroll never produces — e.g. two otherwise-static lines
	// elsewhere on a full screen happening to resemble each other under
	// some small k while the user types on a completely different line.
	//
	// "Confirmed-fixed neighbor" (unshiftedMatch just outside the band on
	// BOTH sides) is the axis-edge condition's stand-in for a window that
	// has its own fixed chrome sandwiching the scrollable content on
	// every side — a winbar above, a status bar and command line below —
	// which never touches row 0 or n-1 at all despite being a completely
	// genuine, unambiguous scroll (this is the common case for any nvim
	// window, split or not, that sets a winbar). Requiring a much larger
	// total here than the bare minLines floor matters: unshiftedMatch is
	// true for almost any UNCHANGED neighboring line, edge or not (most
	// of an ordinary terminal doesn't change between two frames), so on
	// its own it would just as happily "anchor" — and let through — the
	// genuine floating-island false positive too. A real chrome-bounded
	// scroll carries far more evidence than a coincidental 1-2 line
	// match ever does, so minAnchorlessLines is the real gate here, not
	// the neighbor check by itself.
	edgeAnchored := finalLo == 0 || finalHi == n-1
	chromeAnchored := !edgeAnchored && total >= minLines*anchorlessLineFactor &&
		(finalLo == 0 || unshiftedMatch(finalLo-1)) &&
		(finalHi == n-1 || unshiftedMatch(finalHi+1))
	if !edgeAnchored && !chromeAnchored {
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

// cellAffixGap is unshiftedMatch's structural check: the width of
// content between a's and b's longest common (rune) prefix and longest
// common suffix. A real fixed line (a status bar, a ruler) keeps its
// surrounding layout byte-identical and only changes a narrow span
// somewhere — this is small for that case regardless of how many raw
// characters that span happens to contain. Genuinely unrelated content
// (real new content a scroll revealed) typically shares no meaningful
// prefix OR suffix with whatever used to occupy that position, so this
// comes out close to the full length. A length mismatch (shouldn't
// happen — both share the screen's Cols) returns the full length, never
// a coincidental pass.
func cellAffixGap(a, b []Cell) int {
	n := len(a)
	if n == 0 || len(b) != n {
		return n
	}
	prefix := 0
	for prefix < n && a[prefix].Rune == b[prefix].Rune {
		prefix++
	}
	suffix := 0
	for suffix < n-prefix && a[n-1-suffix].Rune == b[n-1-suffix].Rune {
		suffix++
	}
	return n - prefix - suffix
}

// colAffixGap is cellAffixGap's transpose: the same longest-common-
// prefix/suffix gap, computed down column colA of gridA against column
// colB of gridB instead of along a row.
func colAffixGap(gridA, gridB [][]Cell, colA, colB int) int {
	n := len(gridA)
	if n == 0 || len(gridB) != n {
		return n
	}
	prefix := 0
	for prefix < n && gridA[prefix][colA].Rune == gridB[prefix][colB].Rune {
		prefix++
	}
	suffix := 0
	for suffix < n-prefix && gridA[n-1-suffix][colA].Rune == gridB[n-1-suffix][colB].Rune {
		suffix++
	}
	return n - prefix - suffix
}
