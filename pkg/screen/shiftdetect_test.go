package screen

import (
	"fmt"
	"strings"
	"testing"
)

// fillRow writes text's runes into s.Grid[y] starting at column 0,
// leaving any remaining columns as whatever was already there (New's
// blank cells, by default).
func fillRow(s *Screen, y int, text string) {
	for i, r := range []rune(text) {
		s.Grid[y][i].Rune = r
	}
}

func TestDetectContentShiftCleanWholeScreenShift(t *testing.T) {
	prev := New(10, 5)
	next := New(10, 5)
	for y := range 5 {
		fillRow(prev, y, fmt.Sprintf("row%d-----", y))
	}
	for y := range 4 {
		fillRow(next, y, fmt.Sprintf("row%d-----", y+1))
	}
	shift, ok := DetectContentShift(prev, next, 5)
	if !ok {
		t.Fatal("expected a detected shift")
	}
	// Bottom extends to the last row (4), not just the natural overlap's
	// end (3): row 4 is brand-new content the scroll revealed, with no
	// counterpart in prev at all — it belongs in the same glide band as
	// the matched rows above it, not left out to pop in unanimated.
	want := RowShift{Top: 0, Bottom: 4, Left: 0, Right: 9, Delta: 1}
	if shift != want {
		t.Fatalf("shift = %+v, want %+v", shift, want)
	}
}

func TestDetectContentShiftAdversarialGutterNoise(t *testing.T) {
	const cols, rows = 25, 10
	prev := New(cols, rows)
	next := New(cols, rows)
	payload := func(y int) string { return fmt.Sprintf("payload%15d", y) }
	gutterPrev := func(y int) string { return string([]rune{rune('a' + y), rune('a' + y), rune('a' + y)}) }
	gutterNext := func(y int) string { return string([]rune{rune('A' + y), rune('A' + y), rune('A' + y)}) }

	for y := range rows {
		fillRow(prev, y, gutterPrev(y)+payload(y))
	}
	// Shift up by 3: next[y] carries prev[y+3]'s payload, but a
	// different (still 3-char) gutter — simulating relativenumber
	// recomputing on every row after a scroll.
	for y := range 7 {
		fillRow(next, y, gutterNext(y)+payload(y+3))
	}

	shift, ok := DetectContentShift(prev, next, 10)
	if !ok {
		t.Fatal("expected a detected shift despite per-row gutter noise")
	}
	if shift.Delta != 3 {
		t.Fatalf("Delta = %d, want 3", shift.Delta)
	}
	// Bottom extends to the last row (9): rows 7-9 are brand-new content
	// the shift revealed (no prev counterpart), folded into the same
	// band as the matched, gutter-noisy rows 0-6 above them.
	if shift.Top != 0 || shift.Bottom != 9 {
		t.Fatalf("band = [%d,%d], want [0,9]", shift.Top, shift.Bottom)
	}
}

func TestDetectContentShiftFullRepaintReportsNoShift(t *testing.T) {
	prev := New(10, 5)
	next := New(10, 5)
	for y := range 5 {
		fillRow(prev, y, fmt.Sprintf("aaaaaaaaa%d", y))
	}
	for y := range 5 {
		fillRow(next, y, fmt.Sprintf("11111111%d1", y))
	}
	if shift, ok := DetectContentShift(prev, next, 5); ok {
		t.Fatalf("expected no shift, got %+v", shift)
	}
}

func TestDetectContentShiftPartialRegionRespectsFixedHeader(t *testing.T) {
	prev := New(10, 10)
	next := New(10, 10)
	fillRow(prev, 0, "STATUSLINE")
	for y := 1; y < 10; y++ {
		fillRow(prev, y, fmt.Sprintf("body-row-%d", y))
	}
	// Header stays put; rows 1-9 shift up by 2 within that sub-band.
	fillRow(next, 0, "STATUSLINE")
	for y := 1; y <= 7; y++ {
		fillRow(next, y, fmt.Sprintf("body-row-%d", y+2))
	}

	shift, ok := DetectContentShift(prev, next, 10)
	if !ok {
		t.Fatal("expected a detected shift")
	}
	if shift.Delta != 2 {
		t.Fatalf("Delta = %d, want 2", shift.Delta)
	}
	if shift.Top != 1 {
		t.Fatalf("Top = %d, want 1 (fixed header row excluded)", shift.Top)
	}
}

func TestDetectContentShiftExtendsBandToNewTrailingContent(t *testing.T) {
	// 12 rows, shift up by 2: rows 0-9 match (prev[y+2]), rows 10-11 are
	// brand-new content with no prev counterpart at all — the band
	// should extend down to include them rather than leave them to pop
	// in unanimated next to a still-easing band.
	prev := New(10, 12)
	next := New(10, 12)
	for y := range 12 {
		fillRow(prev, y, fmt.Sprintf("row-%02d----", y))
	}
	for y := range 10 {
		fillRow(next, y, fmt.Sprintf("row-%02d----", y+2))
	}
	fillRow(next, 10, "new-line-a")
	fillRow(next, 11, "new-line-b")

	shift, ok := DetectContentShift(prev, next, 12)
	if !ok {
		t.Fatal("expected a detected shift")
	}
	if shift.Delta != 2 {
		t.Fatalf("Delta = %d, want 2", shift.Delta)
	}
	if shift.Top != 0 || shift.Bottom != 11 {
		t.Fatalf("band = [%d,%d], want [0,11] (extended to the new trailing rows)", shift.Top, shift.Bottom)
	}
}

func TestDetectContentShiftDoesNotExtendPastARealFixedLine(t *testing.T) {
	// Same shape as above, but row 9 (right at the edge of the natural
	// overlap) is a genuine fixed/unrelated line, not new content — the
	// band must stop at the trim, not extend past it into rows 10-11
	// even though those really are new content.
	prev := New(10, 12)
	next := New(10, 12)
	for y := range 12 {
		fillRow(prev, y, fmt.Sprintf("row-%02d----", y))
	}
	for y := range 9 {
		fillRow(next, y, fmt.Sprintf("row-%02d----", y+2))
	}
	fillRow(next, 9, "FIXEDLINE-")
	fillRow(next, 10, "new-line-a")
	fillRow(next, 11, "new-line-b")

	shift, ok := DetectContentShift(prev, next, 12)
	if !ok {
		t.Fatal("expected a detected shift")
	}
	if shift.Bottom != 8 {
		t.Fatalf("Bottom = %d, want 8 (a real fixed line at the overlap edge blocks extension)", shift.Bottom)
	}
}

// TestDetectContentShiftIgnoresBlankPaddingDuringTyping guards against a
// real regression: a shell prompt with one line of real text on an
// otherwise blank terminal (the overwhelmingly common case) has dozens
// of blank rows that trivially "match" each other at almost any
// candidate shift — without excluding blank-vs-blank pairs from
// counting as evidence, typing a single extra character fired a
// spurious one-line shift on every keystroke.
func TestDetectContentShiftIgnoresBlankPaddingDuringTyping(t *testing.T) {
	prev := New(40, 24)
	next := New(40, 24)
	fillRow(prev, 5, "$ echo hell")
	fillRow(next, 5, "$ echo hello")
	if shift, ok := DetectContentShift(prev, next, 10); ok {
		t.Fatalf("expected no shift from typing on one line, got %+v", shift)
	}
}

func TestDetectHorizontalContentShiftIgnoresBlankPaddingDuringTyping(t *testing.T) {
	prev := New(40, 24)
	next := New(40, 24)
	fillRow(prev, 5, "$ echo hell")
	fillRow(next, 5, "$ echo hello")
	if shift, ok := DetectHorizontalContentShift(prev, next, 10); ok {
		t.Fatalf("expected no horizontal shift from typing on one line, got %+v", shift)
	}
}

// TestDetectContentShiftIgnoresSharedPaddingBetweenShortCommands guards
// against a real regression: a realistic shell — a wide terminal, taller
// than the visible history, with 20 lines of short, genuinely different
// commands (each far shorter than the terminal's width, so most of
// every row is shared blank padding out to the edge) plus blank rows
// below. Typing one more character on the active last line must not
// wobble anything else: two unrelated short commands sharing all that
// blank padding must not read as "differing by only a few characters."
func TestDetectContentShiftIgnoresSharedPaddingBetweenShortCommands(t *testing.T) {
	const cols, rows = 80, 30
	commands := []string{
		"$ ls", "$ cd ..", "$ pwd", "$ git status", "$ cat file.txt",
		"$ echo hi", "$ whoami", "$ date", "$ clear", "$ history",
		"$ top", "$ df -h", "$ du -sh .", "$ uptime", "$ ps aux",
		"$ which go", "$ go version", "$ ls -la", "$ cd /tmp", "$ exit",
	}
	prev := New(cols, rows)
	next := New(cols, rows)
	for y, cmd := range commands {
		fillRow(prev, y, cmd)
		fillRow(next, y, cmd)
	}
	// The active prompt (row len(commands)) gets one more character —
	// everything else, including the blank rows below it, is untouched.
	fillRow(next, len(commands), "$ x")

	if shift, ok := DetectContentShift(prev, next, 10); ok {
		t.Fatalf("expected no shift from typing on the active line, got %+v", shift)
	}
}

// TestDetectContentShiftRejectsFloatingMiddleIsland guards against a
// real regression: a full screen (no blank padding to exclude) where
// only the last line changes (the user typing at the shell prompt).
// Rows 7-9 are deliberately identical to each other, which produces a
// coincidental 2-row match under a small shift k somewhere in the
// middle of the screen — a real scroll always originates from an edge,
// so a band touching neither edge must be rejected rather than
// animated as a spurious "something moving in the middle" wobble.
func TestDetectContentShiftRejectsFloatingMiddleIsland(t *testing.T) {
	const cols, rows = 24, 20
	// Genuinely distinct words per row (not a shared prefix plus an
	// incrementing digit) — content differing only in a trailing
	// counter is legitimately within gutter-tolerance range for ANY
	// reasonable per-line heuristic, so it can't isolate a truly
	// coincidental match from real gutter-style noise. These words
	// differ from their neighbors by many characters.
	words := []string{
		"apple", "banana", "cherry", "dragon", "eagle", "forest", "guitar",
		"harbor", "island", "jungle", "kitten", "lumber", "meadow", "nectar",
		"oyster", "planet", "quartz", "rocket", "sunset", "temple",
	}
	lineContent := func(y int) string {
		if y >= 7 && y <= 9 {
			return "REPEATED-LINE-XYZ"
		}
		return "prompt-" + words[y%len(words)] + "-output"
	}
	prev := New(cols, rows)
	next := New(cols, rows)
	for y := range rows {
		fillRow(prev, y, lineContent(y))
		fillRow(next, y, lineContent(y))
	}
	// Only the last line changes — the user typing at the prompt.
	fillRow(next, rows-1, lineContent(rows-1)+"x")

	if shift, ok := DetectContentShift(prev, next, 10); ok {
		t.Fatalf("expected no shift (floating middle island), got %+v", shift)
	}
}

func TestDetectContentShiftGuardsMismatchedDimensions(t *testing.T) {
	prev := New(10, 5)
	next := New(12, 5)
	if shift, ok := DetectContentShift(prev, next, 5); ok {
		t.Fatalf("expected no shift for mismatched dimensions, got %+v", shift)
	}
}

func TestDetectHorizontalContentShiftCleanShift(t *testing.T) {
	// Wide enough (30 cols) that the shifted band clears
	// minShiftBandColumns — the horizontal axis requires substantially
	// more informative content than the row axis to be reliable (see
	// minShiftBandColumns's doc), so a narrow fixture can't exercise it.
	const cols = 30
	prev := New(cols, 4)
	next := New(cols, 4)
	for y := range 4 {
		for x := range cols {
			prev.Grid[y][x].Rune = rune('a' + x%26)
		}
	}
	// Shift left by 2: next column x carries prev column x+2's content
	// (same rune per row, since every row in this fixture is identical).
	for y := range 4 {
		for x := range cols - 2 {
			next.Grid[y][x].Rune = rune('a' + (x+2)%26)
		}
	}
	shift, ok := DetectHorizontalContentShift(prev, next, 5)
	if !ok {
		t.Fatal("expected a detected horizontal shift")
	}
	if shift.Delta != 2 {
		t.Fatalf("Delta = %d, want 2", shift.Delta)
	}
}

func TestDetectHorizontalContentShiftWithFixedGutterColumn(t *testing.T) {
	// Wide enough that the shifted band (columns 1..cols-1, minus the
	// shift/trim overhead) still clears minShiftBandColumns.
	const cols, rows = 34, 10
	prev := New(cols, rows)
	next := New(cols, rows)
	// Column 0 is a fixed marker column that never shifts; columns
	// 1..11 shift left by 2, with a recomputed (noisy) row 0..7 marker
	// in that fixed column changing between prev/next (simulating a
	// per-row indicator, mirroring the vertical gutter case).
	for y := range rows {
		prev.Grid[y][0].Rune = rune('a' + y)
		for x := 1; x < cols; x++ {
			prev.Grid[y][x].Rune = rune('A' + x)
		}
	}
	for y := range rows {
		next.Grid[y][0].Rune = rune('Z' - y)
		for x := 1; x <= cols-3; x++ {
			next.Grid[y][x].Rune = rune('A' + x + 2)
		}
	}
	shift, ok := DetectHorizontalContentShift(prev, next, 6)
	if !ok {
		t.Fatal("expected a detected horizontal shift despite the fixed marker column")
	}
	if shift.Delta != 2 {
		t.Fatalf("Delta = %d, want 2", shift.Delta)
	}
	if shift.Left < 1 {
		t.Fatalf("Left = %d, want >=1 (fixed marker column excluded)", shift.Left)
	}
}

func TestDetectHorizontalContentShiftFullRepaintReportsNoShift(t *testing.T) {
	prev := New(10, 5)
	next := New(10, 5)
	for y := range 5 {
		for x := range 10 {
			prev.Grid[y][x].Rune = rune('a' + x)
			next.Grid[y][x].Rune = rune('0' + (x+y)%10)
		}
	}
	if shift, ok := DetectHorizontalContentShift(prev, next, 5); ok {
		t.Fatalf("expected no shift, got %+v", shift)
	}
}

func TestDetectContentShiftLowConfidenceBoundary(t *testing.T) {
	// A band where roughly half the rows agree with the candidate shift
	// and half don't — below confidenceThreshold, so no shift should be
	// reported even though a partial match exists.
	prev := New(10, 8)
	next := New(10, 8)
	for y := range 8 {
		fillRow(prev, y, fmt.Sprintf("uniquerow%d", y))
	}
	for y := range 7 {
		if y%2 == 0 {
			fillRow(next, y, fmt.Sprintf("uniquerow%d", y+1))
		} else {
			fillRow(next, y, fmt.Sprintf("nomatch%d__", y))
		}
	}
	if shift, ok := DetectContentShift(prev, next, 8); ok {
		t.Fatalf("expected no shift below confidence threshold, got %+v", shift)
	}
}

// TestDetectContentShiftRejectsColumnarLookalikes guards against tier 2
// (the fixed-absolute-count fuzzy fallback) becoming, by itself, enough
// evidence for a shift. Columnar/tabular terminal output — `ls -la`, ps,
// git log --oneline, df — has consecutive UNRELATED rows sharing the
// same fixed layout, differing only by a short token (a date, a name):
// almost always within any reasonable fixed maxDiff regardless of how
// long the lines are. Nothing here actually scrolled — only row 3 was
// edited in place — so no shift, at any k, should ever be reported; a
// prior version of DetectShift let tier 2 alone report a whole-screen
// false shift here since every row happened to clear fuzzyRowMaxDiff
// against its neighbor purely from shared layout, with zero tier-1
// (exact/aux) corroboration anywhere in the band.
func TestDetectContentShiftRejectsColumnarLookalikes(t *testing.T) {
	const cols, rows = 60, 8
	prev := New(cols, rows)
	next := New(cols, rows)
	mkLine := func(day int, name string) string {
		return fmt.Sprintf("drwxr-xr-x  2 mo mo 4096 Sep 14 10:%02d project-%s", day, name)
	}
	names := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel"}
	for y := range rows {
		fillRow(prev, y, mkLine(20+y, names[y]))
		fillRow(next, y, mkLine(20+y, names[y]))
	}
	// Simulate an in-place edit (e.g. a re-stat after touch) — not a
	// scroll at all.
	fillRow(next, 3, mkLine(59, names[3]))

	if shift, ok := DetectContentShift(prev, next, rows-1); ok {
		t.Fatalf("false positive: detected shift %+v where nothing scrolled (only row 3 changed in place)", shift)
	}
}

// TestDetectHorizontalContentShiftRejectsRepeatedRule guards against a
// themed prompt's horizontal rule/separator (Starship, Powerlevel10k,
// oh-my-posh, and tmux status bars all print one) causing a false
// horizontal shift on ordinary typing. A run of the same repeated
// character makes many COLUMNS' hashes genuinely, exactly identical —
// not just similar — so any shift within that run trivially clears tier
// 1 no matter how tight fuzzyColMaxDiff/confidenceColThreshold are; the
// tier-1-ratio floor alone doesn't catch this since the false matches
// really are tier 1, not tier 2's fuzzy fallback. Only the last row
// changes here (one keystroke); nothing scrolled.
func TestDetectHorizontalContentShiftRejectsRepeatedRule(t *testing.T) {
	const cols, rows = 100, 24
	prev := New(cols, rows)
	fillRow(prev, 0, strings.Repeat("─", 90))
	fillRow(prev, 1, "~/code/tubeless on  main [!?] via 🐹 v1.22")
	fillRow(prev, 2, "$ ")

	next := New(cols, rows)
	for y := range rows {
		copy(next.Grid[y], prev.Grid[y])
	}
	fillRow(next, 2, "$ l")

	if shift, ok := DetectHorizontalContentShift(prev, next, cols-1); ok {
		t.Fatalf("false positive: detected horizontal shift %+v from a single keystroke (ruler row present)", shift)
	}
}

// TestDetectContentShiftFindsSplitPaneScroll is the permanent regression
// for the actual bug: "no smooth scrolling inside an nvim pane" (live
// PTY-driven nvim confirmed this exact shape during development — see
// git history). A vertical split's divider column glues two otherwise
// unrelated panes into the same physical rows, so a whole-row hash never
// matches even when one pane cleanly scrolled — DetectContentShift must
// fall back to columnBands and search the scrolled pane's own column
// range independently.
func TestDetectContentShiftFindsSplitPaneScroll(t *testing.T) {
	const cols, rows = 40, 10
	prev := New(cols, rows)
	next := New(cols, rows)

	// Distinct per-row text on the left pane too (short: must not cross
	// the divider at col 18) — a template differing by one digit would
	// fuzzy-match its own shifted self across candidates just as easily
	// as the right pane's real content does, defeating the "frozen,
	// unrelated" premise this test needs.
	leftWords := []string{
		"import fmt", "package main", "type T struct", "func main()",
		"x := 1", "y := 2", "if x > y", "return x", "} else {",
		"log.Fatal(e)", "for range xs", "ch <- v",
	}
	leftLine := func(y int) string { return leftWords[y%len(leftWords)] }
	// Distinct per-row text, not a shared template differing by one
	// digit — otherwise adjacent rows are nearly identical to EACH
	// OTHER too, making a shift-by-1 candidate look almost as good as
	// the real shift-by-2 (the same ambiguity TestDetectContentShift
	// RejectsColumnarLookalikes guards elsewhere).
	rightWords := []string{
		"func handleRequest(ctx)", "var status = pending", "return nil, err",
		"defer conn.Close()", "log.Printf(startup)", "if user == admin",
		"for i, v := range xs", "switch cmd.Type", "case scrollUp:",
		"go worker.Run(wg)", "chan struct{}{}", "mutex.Lock()",
	}
	rightLine := func(y int) string { return rightWords[y%len(rightWords)] }

	for y := range rows {
		fillRow(prev, y, leftLine(y))
		prev.Grid[y][18].Rune = '│'
		fillRow(next, y, leftLine(y)) // left pane: frozen, identical
		next.Grid[y][18].Rune = '│'
	}
	for y := range rows {
		copy(prev.Grid[y][19:], mustCells(rightLine(y), cols-19))
	}
	// Right pane scrolled up by 2: next[y] carries prev[y+2]'s content;
	// the last 2 rows are freshly revealed, not stale prev content.
	for y := range rows {
		copy(next.Grid[y][19:], mustCells(rightLine(y+2), cols-19))
	}

	shift, ok := DetectContentShift(prev, next, rows-1)
	if !ok {
		t.Fatal("expected the split-pane scroll to be detected via the column-band fallback")
	}
	if shift.Delta != 2 {
		t.Fatalf("Delta = %d, want 2", shift.Delta)
	}
	if shift.Left < 19 {
		t.Fatalf("shift.Left = %d, leaked into the frozen left pane (divider at col 18)", shift.Left)
	}
}

// mustCells renders text into a fresh []Cell of length n (space-padded),
// for building a sub-slice of a row directly.
func mustCells(text string, n int) []Cell {
	cells := make([]Cell, n)
	for i := range cells {
		cells[i].Rune = ' '
	}
	for i, r := range []rune(text) {
		if i >= n {
			break
		}
		cells[i].Rune = r
	}
	return cells
}

// TestDetectContentShiftIgnoresStaticSplitDivider guards columnBands
// itself: a vertical divider with nothing scrolling on either side must
// not manufacture a shift out of nowhere.
func TestDetectContentShiftIgnoresStaticSplitDivider(t *testing.T) {
	const cols, rows = 40, 10
	prev := New(cols, rows)
	for y := range rows {
		fillRow(prev, y, fmt.Sprintf("left %d", y))
		prev.Grid[y][18].Rune = '│'
		copy(prev.Grid[y][19:], mustCells(fmt.Sprintf("right %d", y), cols-19))
	}
	next := New(cols, rows)
	for y := range rows {
		copy(next.Grid[y], prev.Grid[y])
	}

	if shift, ok := DetectContentShift(prev, next, rows-1); ok {
		t.Fatalf("false positive: detected shift %+v on a fully static split screen", shift)
	}
}

// TestDetectContentShiftDoesNotExtendIntoStatusBar is the permanent
// regression for a real bug: a status bar pinned to the second-to-last
// row (nvim's default layout — buffer, statusline, command line) was
// getting swept into the "extend to new trailing content" band whenever
// the scroll delta was larger than a line or two, because the status
// bar's own row also has no counterpart under the shift (same as
// genuinely revealed content does) — it visibly slid and snapped back
// on every scroll. A status bar's content barely changes frame to frame
// (only a line/col counter ticks over), which is exactly what
// unshiftedMatch uses to tell it apart from real new content.
func TestDetectContentShiftDoesNotExtendIntoStatusBar(t *testing.T) {
	const cols, rows = 60, 20
	bufWords := []string{
		"func main() {", "\tvar x int", "\tfor i := range 10 {",
		"\t\tx += i", "\t}", "\tfmt.Println(x)", "}", "",
		"type T struct{}", "func (t T) Do() {}", "var global = 1",
		"const K = 2", "package util", "import \"fmt\"",
		"// a comment line", "func helper() int { return 1 }",
		"if err != nil {", "\treturn err", "}", "// end of file",
	}
	statusBar := func(line int) string { return fmt.Sprintf("main.go [+]%*s%d,1  %d%%", 30, "", line, line) }

	prev := New(cols, rows)
	next := New(cols, rows)
	for y := 0; y < rows-1; y++ {
		fillRow(prev, y, bufWords[y%len(bufWords)])
		fillRow(next, y, bufWords[y%len(bufWords)])
	}
	fillRow(prev, rows-1, statusBar(10))
	fillRow(next, rows-1, statusBar(28)) // cursor moved: only the counter changed

	// Buffer scrolled up by 8; the status bar (row 19) must NOT be
	// dragged into the band even though it also lacks a prev counterpart
	// at the shifted index.
	for y := 0; y < rows-1-8; y++ {
		fillRow(next, y, bufWords[(y+8)%len(bufWords)])
	}

	shift, ok := DetectContentShift(prev, next, rows-1)
	if !ok {
		t.Fatal("expected the buffer scroll to be detected")
	}
	if shift.Bottom >= rows-1 {
		t.Fatalf("Bottom = %d, swallowed the status bar row %d", shift.Bottom, rows-1)
	}
}

// TestDetectContentShiftFindsChromeSandwichedScroll is the permanent
// regression for the real bug this session found live: a window with
// its own winbar (top) AND status bar + command line (bottom) — a
// completely ordinary nvim layout with 'winbar' set — never touches row
// 0 or row n-1 at all, no matter how real and uniform the buffer's own
// scroll is. The pre-existing "floating island" edge-anchor rejection
// (meant to catch a coincidental mid-screen match) doesn't know the
// difference and rejected this too until chromeAnchored was added.
func TestDetectContentShiftFindsChromeSandwichedScroll(t *testing.T) {
	const cols, rows = 60, 24
	bufWords := []string{
		"func handleRequest(ctx)", "\tuser := auth.From(ctx)", "\tif user == nil {",
		"\t\treturn errUnauth", "\t}", "\tdata := store.Get(id)", "\treturn data, nil",
		"}", "", "func main() {", "\tmux := http.NewServeMux()",
		"\tmux.Handle(\"/api\", h)", "\tlog.Fatal(srv.ListenAndServe())", "}",
		"type Server struct {", "\tmux *http.ServeMux", "\taddr string", "}",
		"func New() *Server {", "\treturn &Server{}", "}", "// trailing comment",
		"var _ = 1", "const X = 2", "package main",
	}
	winbar := func(line int) string { return fmt.Sprintf("main.go %d:1", line) }
	statusBar := func(pct int) string { return fmt.Sprintf("main.go [+]%*s%d%%", 30, "", pct) }

	prev := New(cols, rows)
	next := New(cols, rows)
	// Row 0: winbar (barely changes — just the cursor line indicator).
	fillRow(prev, 0, winbar(10))
	fillRow(next, 0, winbar(28))
	// Rows 1..rows-3: the buffer, scrolled up by 8.
	for y := 1; y < rows-2; y++ {
		fillRow(prev, y, bufWords[y%len(bufWords)])
		fillRow(next, y, bufWords[(y+8)%len(bufWords)])
	}
	// Row rows-2: status bar (barely changes). Row rows-1: command line
	// (empty in both — untouched).
	fillRow(prev, rows-2, statusBar(10))
	fillRow(next, rows-2, statusBar(45))

	shift, ok := DetectContentShift(prev, next, rows-1)
	if !ok {
		t.Fatal("expected the chrome-sandwiched buffer scroll to be detected")
	}
	if shift.Top != 1 {
		t.Fatalf("Top = %d, want 1 (winbar at row 0 excluded)", shift.Top)
	}
	if shift.Bottom >= rows-2 {
		t.Fatalf("Bottom = %d, swallowed the status bar row %d", shift.Bottom, rows-2)
	}
	if shift.Delta != 8 {
		t.Fatalf("Delta = %d, want 8", shift.Delta)
	}
}

// TestDetectContentShiftSurvivesRulerDigitReflow is the permanent
// regression for the live "arrow-key scroll drags the status bar"
// report: a per-window ruler/statusline (nvim's default, one per split
// pane at laststatus=2) whose position indicator changes shape between
// frames — "39,0-1  0%" to "40,1  1%" here, captured verbatim from a
// real nvim session — differs by 6 raw characters, MORE than the
// unrelated-content case in TestDetectContentShiftExtendsBandToNewTrailingContent
// (7 mismatches) is allowed to differ by under the old fixed-count
// check, which made a single mismatch threshold unable to tell them
// apart. What actually separates them: the ruler keeps its entire
// surrounding layout (filename, padding) byte-identical and only
// changes one small span in the middle, while genuinely new content
// shares no meaningful prefix or suffix with whatever was there before.
func TestDetectContentShiftSurvivesRulerDigitReflow(t *testing.T) {
	const cols, rows = 69, 10
	bufWords := []string{
		"func handleRequest(ctx)", "\tuser := auth.From(ctx)", "\tif user == nil {",
		"\t\treturn errUnauth", "\t}", "\treturn data, nil", "}", "",
		"func main() {", "package main",
	}
	ruler := "personal/tubeless/cmd/tubeless/main.go             39,0-1          0%"
	rulerNext := "personal/tubeless/cmd/tubeless/main.go             40,1            1%"

	prev := New(cols, rows)
	next := New(cols, rows)
	for y := 0; y < rows-1; y++ {
		fillRow(prev, y, bufWords[y%len(bufWords)])
		fillRow(next, y, bufWords[(y+1)%len(bufWords)]) // scrolled up by 1
	}
	fillRow(prev, rows-1, ruler)
	fillRow(next, rows-1, rulerNext)

	shift, ok := DetectContentShift(prev, next, rows-1)
	if !ok {
		t.Fatal("expected the buffer scroll to be detected despite the ruler's digit reflow")
	}
	if shift.Bottom >= rows-1 {
		t.Fatalf("Bottom = %d, swallowed the ruler row %d", shift.Bottom, rows-1)
	}
	if shift.Delta != 1 {
		t.Fatalf("Delta = %d, want 1", shift.Delta)
	}
}

// TestColumnBandsIgnoresPartialTreeIndentGuide is the permanent
// regression for a real bug: a file-tree sidebar (neo-tree, nvim-tree)
// draws its own decorative indent guides using the same glyph as a real
// window border, but only on rows whose file happens to be nested —
// which row that is changes as the tree scrolls. At a lenient ratio,
// this column crossed the divider threshold in either direction almost
// every frame, so columnBands reported a different (wrong) pane
// boundary on nearly every scroll — read live as the glide resetting on
// every keystroke. A real window border is interrupted only by that
// window's own chrome rows, not by roughly a third of ordinary content
// rows, so a strict ratio must still find it while rejecting the guide.
func TestColumnBandsIgnoresPartialTreeIndentGuide(t *testing.T) {
	const cols, rows = 44, 20
	prev := New(cols, rows)
	next := New(cols, rows)
	for y := range rows {
		fillRow(prev, y, fmt.Sprintf("  file-%02d", y))
		fillRow(next, y, fmt.Sprintf("  file-%02d", y))
		// A real window border: every row, both frames.
		prev.Grid[y][39].Rune = '│'
		next.Grid[y][39].Rune = '│'
		// A decorative indent guide: only on 15 of 20 rows (75%) —
		// below dividerRowRatio but was above the old, looser one.
		if y%4 != 0 {
			prev.Grid[y][3].Rune = '│'
			next.Grid[y][3].Rune = '│'
		}
	}

	bands := columnBands(prev, next)
	want := [][2]int{{0, 38}, {40, 43}}
	if len(bands) != len(want) {
		t.Fatalf("bands = %+v, want %+v (indent guide at col 3 must not split the band)", bands, want)
	}
	for i := range want {
		if bands[i] != want[i] {
			t.Fatalf("bands = %+v, want %+v", bands, want)
		}
	}
}
