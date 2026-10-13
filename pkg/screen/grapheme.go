package screen

import "github.com/rivo/uniseg"

// firstCombiningRune is U+0300, the start of the combining diacritics
// block. No rune below it ever extends a grapheme cluster, so plain
// ASCII/Latin output skips the cluster check entirely.
const firstCombiningRune = 0x300

// runeWidth is how many columns r takes on its own: 2 for emoji with
// emoji presentation and East Asian wide/fullwidth runes, 0 for
// zero-width runes, 1 for everything else.
func runeWidth(r rune) int {
	if r < firstCombiningRune {
		return 1
	}
	return uniseg.StringWidth(string(r))
}

// joinPrevious appends r to the previous cell's grapheme cluster when r
// extends it (a VS16, a skin tone, a ZWJ and the emoji after it, a
// flag's second regional indicator, a combining accent). A cluster that
// grows from 1 column to 2 (❤ + VS16 is ❤️) widens its cell in place.
func (s *Screen) joinPrevious(r rune) bool {
	if r < firstCombiningRune {
		return false
	}
	x, ok := s.previousCellX()
	if !ok {
		return false
	}
	cell := &s.Grid[s.CursorY][x]
	cluster := cell.Text() + string(r)
	if uniseg.GraphemeClusterCount(cluster) != 1 {
		return false
	}
	cell.Tail += string(r)
	if uniseg.StringWidth(cluster) == 2 {
		s.widenPrevious(x)
	}
	return true
}

// previousCellX is the column of the cell the last Put wrote on the
// cursor row: the cursor cell itself while a wrap is pending, else the
// one to its left, stepping over a WideSpacer onto its owner.
func (s *Screen) previousCellX() (int, bool) {
	if s.pendingWrap {
		return s.CursorX, true
	}
	if s.CursorX == 0 {
		return 0, false
	}
	x := s.CursorX - 1
	if s.Grid[s.CursorY][x].Rune == WideSpacer && x > 0 {
		x--
	}
	return x, true
}

// widenPrevious gives the 1-column cell at x, just left of the cursor, a
// WideSpacer at the cursor and moves the cursor past it. A cell that is
// already wide, or sits in the last column, stays as it is.
func (s *Screen) widenPrevious(x int) {
	if s.pendingWrap || x+1 != s.CursorX {
		return
	}
	attr := s.Grid[s.CursorY][x].Attr
	s.putCell(Cell{Rune: WideSpacer, Attr: attr})
	s.advanceCursor()
}

// putCell writes c at the cursor. Overwriting one half of an older wide
// cell blanks its other half, so no orphan glyph or spacer is left
// behind. A WideSpacer's own left neighbor is the new owner, so only a
// non-spacer write looks left.
func (s *Screen) putCell(c Cell) {
	row := s.Grid[s.CursorY]
	x := s.CursorX
	if c.Rune != WideSpacer && row[x].Rune == WideSpacer && x > 0 {
		row[x-1] = Cell{Rune: ' ', Attr: row[x-1].Attr}
	}
	if x+1 < s.Cols && row[x+1].Rune == WideSpacer {
		row[x+1] = Cell{Rune: ' ', Attr: row[x+1].Attr}
	}
	row[x] = c
}
