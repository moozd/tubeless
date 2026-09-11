package screen

import (
	"fmt"
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
	want := RowShift{Top: 0, Bottom: 4, Delta: 1}
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
