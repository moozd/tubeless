package screen

// InsertLines inserts n blank lines at the cursor row, shifting the rows
// below it (within the scroll region) down and dropping whatever falls
// off ScrollBottom. A no-op outside the scroll region, matching real
// terminal behavior — full-screen apps rely on that to redraw only the
// region they mean to.
func (s *Screen) InsertLines(n int) {
	if s.CursorY < s.ScrollTop || s.CursorY > s.ScrollBottom {
		return
	}
	top, bottom := s.CursorY, s.ScrollBottom
	n = min(n, bottom-top+1)
	copy(s.Grid[top+n:bottom+1], s.Grid[top:bottom-n+1])
	for y := top; y < top+n; y++ {
		s.Grid[y] = s.blankRow()
	}
	s.pendingScrolls = append(s.pendingScrolls, ScrollShift{Top: top, Bottom: bottom, Delta: -n})
}

// DeleteLines removes n lines at the cursor row, shifting the rows below
// it (within the scroll region) up and filling the gap at ScrollBottom
// with blanks.
func (s *Screen) DeleteLines(n int) {
	if s.CursorY < s.ScrollTop || s.CursorY > s.ScrollBottom {
		return
	}
	top, bottom := s.CursorY, s.ScrollBottom
	n = min(n, bottom-top+1)
	copy(s.Grid[top:bottom-n+1], s.Grid[top+n:bottom+1])
	for y := bottom - n + 1; y <= bottom; y++ {
		s.Grid[y] = s.blankRow()
	}
	s.pendingScrolls = append(s.pendingScrolls, ScrollShift{Top: top, Bottom: bottom, Delta: n})
}

// InsertChars inserts n blank cells at the cursor column, shifting the
// rest of the row right and dropping whatever falls off the right edge.
func (s *Screen) InsertChars(n int) {
	row := s.Grid[s.CursorY]
	n = min(n, s.Cols-s.CursorX)
	copy(row[s.CursorX+n:], row[s.CursorX:s.Cols-n])
	for x := s.CursorX; x < s.CursorX+n; x++ {
		row[x] = erasedCell(s.CurAttr)
	}
}

// DeleteChars removes n cells at the cursor column, shifting the rest of
// the row left and filling the gap at the right edge with blanks.
func (s *Screen) DeleteChars(n int) {
	row := s.Grid[s.CursorY]
	n = min(n, s.Cols-s.CursorX)
	copy(row[s.CursorX:s.Cols-n], row[s.CursorX+n:])
	for x := s.Cols - n; x < s.Cols; x++ {
		row[x] = erasedCell(s.CurAttr)
	}
}

// EraseChars blanks n cells starting at the cursor column, without
// shifting anything — unlike DeleteChars, cells past the erased range
// keep their position.
func (s *Screen) EraseChars(n int) {
	row := s.Grid[s.CursorY]
	end := min(s.CursorX+n, s.Cols)
	for x := s.CursorX; x < end; x++ {
		row[x] = erasedCell(s.CurAttr)
	}
}
