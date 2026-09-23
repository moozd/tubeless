package screen

// EraseMode mirrors the CSI Ps argument for erase-in-line/display: 0 = to
// end, 1 = to start, 2 = whole.
type EraseMode int

const (
	EraseToEnd EraseMode = iota
	EraseToStart
	EraseAll
)

func (s *Screen) EraseInLine(mode EraseMode) {
	row := s.Grid[s.CursorY]
	from, to := eraseBounds(mode, s.CursorX, s.Cols-1)
	for x := from; x <= to; x++ {
		row[x] = erasedCell(s.CurAttr)
	}
	// A sixel image anchored on this row is tracked only by row (see
	// PlacedImage), not by the columns it covers, so any erase touching
	// the row drops it rather than leaving it ghosting over whatever
	// replaces the line. This is what makes `clear` work inside tmux:
	// tmux never emits a bare full-screen ED to the outer terminal (that
	// would also blank any other pane sharing the physical screen), so a
	// pane-wide clear reaches here as a run of per-line EL instead of the
	// EraseInDisplay this row would otherwise need to hit.
	kept := s.Images[:0]
	for _, im := range s.Images {
		if im.Row != s.CursorY {
			kept = append(kept, im)
		}
	}
	s.Images = kept
}

func (s *Screen) EraseInDisplay(mode EraseMode) {
	from, to := eraseBounds(mode, s.CursorY, s.Rows-1)
	for y := from; y <= to; y++ {
		s.Grid[y] = s.blankRow()
	}
	if mode == EraseToEnd {
		s.EraseInLine(EraseToEnd)
	}
	if mode == EraseToStart {
		s.EraseInLine(EraseToStart)
	}
	// Erasing display rows takes the text with it; sixel images anchored in
	// the erased span must go too, or they stay floating over blank rows.
	kept := s.Images[:0]
	for _, im := range s.Images {
		if im.Row < from || im.Row > to {
			kept = append(kept, im)
		}
	}
	s.Images = kept
}

func eraseBounds(mode EraseMode, cur, max int) (int, int) {
	switch mode {
	case EraseToStart:
		return 0, cur
	case EraseAll:
		return 0, max
	default:
		return cur, max
	}
}
