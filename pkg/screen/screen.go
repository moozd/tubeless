package screen

// Screen is a cell grid with cursor and scroll-region state. A single
// Screen is mutated by exactly one goroutine (the PTY-input coordinator);
// cross-goroutine sharing works by publishing immutable Clone()s for the
// render loop to read, rather than locking — see cmd/tubeless for the
// publish/load wiring.
type Screen struct {
	Cols, Rows    int
	Grid          [][]Cell
	CursorX       int
	CursorY       int
	CursorVisible bool
	CurAttr       Attr
	ScrollTop     int
	ScrollBottom  int
	OriginMode    bool
	AutoWrap      bool
	pendingWrap   bool
	g0, g1        Charset
	usingG1       bool
	saved         savedCursor
	Images        []PlacedImage

	// altGrid backs the alternate screen buffer (CSI ?47/1047/1049h) —
	// what full-screen apps like vim, less, and lazygit draw into so
	// their output never touches (or needs to restore) scrollback. Grid
	// and altGrid are swapped in EnterAltScreen/ExitAltScreen rather than
	// copied, so switching is O(1) either direction.
	altGrid  [][]Cell
	usingAlt bool
	altSaved savedCursor
}

type savedCursor struct {
	x, y int
	attr Attr
}

func New(cols, rows int) *Screen {
	s := &Screen{Cols: cols, Rows: rows, AutoWrap: true, CursorVisible: true}
	s.ScrollBottom = rows - 1
	s.Grid = make([][]Cell, rows)
	for y := range s.Grid {
		s.Grid[y] = newRow(cols)
	}
	return s
}

// Clone returns an independent deep copy, safe to hand to another
// goroutine (e.g. published for the render loop) while this Screen keeps
// being mutated.
func (s *Screen) Clone() *Screen {
	c := *s
	c.Grid = make([][]Cell, len(s.Grid))
	for y, row := range s.Grid {
		c.Grid[y] = append([]Cell(nil), row...)
	}
	c.Images = append([]PlacedImage(nil), s.Images...)
	return &c
}

// Reset restores a freshly-initialized state at the current size (RIS).
func (s *Screen) Reset() {
	cols, rows := s.Cols, s.Rows
	*s = Screen{Cols: cols, Rows: rows, AutoWrap: true, CursorVisible: true}
	s.ScrollBottom = rows - 1
	s.Grid = make([][]Cell, rows)
	for y := range s.Grid {
		s.Grid[y] = newRow(cols)
	}
}

// Resize reallocates the grid to cols x rows, preserving whatever overlaps
// the old and new size and clamping cursor/scroll-region state to fit.
// cols/rows are clamped to at least 1 — a window minimized or dragged to a
// sliver must never produce a degenerate, unusable grid. The alternate
// screen buffer, if allocated, is resized alongside the active one so a
// full-screen app resumes at the right size if the terminal is resized
// while it's running.
func (s *Screen) Resize(cols, rows int) {
	cols, rows = max(1, cols), max(1, rows)
	s.Grid = resizeGrid(s.Grid, cols, rows)
	if s.altGrid != nil {
		s.altGrid = resizeGrid(s.altGrid, cols, rows)
	}
	s.Cols, s.Rows = cols, rows
	s.ScrollTop = clamp(s.ScrollTop, 0, rows-1)
	s.ScrollBottom = rows - 1
	s.clampCursor()
	// A resize reflows the grid; an image anchored to an old row/column
	// layout no longer sits where it was drawn (and its row may not even
	// exist anymore). Drop them rather than leave sixels ghosting over
	// resized content.
	s.Images = nil
}

func resizeGrid(old [][]Cell, cols, rows int) [][]Cell {
	grid := make([][]Cell, rows)
	for y := range grid {
		grid[y] = newRow(cols)
		if y < len(old) {
			copy(grid[y], old[y])
		}
	}
	return grid
}

// EnterAltScreen switches to the alternate screen buffer (CSI ?1049h),
// saving the cursor and starting from a blank grid — what full-screen
// apps (vim, less, lazygit) expect so their output never lands in
// scrollback. A no-op if already on the alternate buffer.
func (s *Screen) EnterAltScreen() {
	if s.usingAlt {
		return
	}
	s.altSaved = savedCursor{x: s.CursorX, y: s.CursorY, attr: s.CurAttr}
	// Always start from a fresh blank grid, not just the first time: once
	// an app has used the alternate buffer and exited it, s.altGrid still
	// holds that app's last-drawn content. Without reallocating here, the
	// next app to enter the alternate buffer (even a different one) would
	// swap straight into that stale frame and only overwrite it cell by
	// cell as it draws — visible as the previous full-screen app's frozen
	// content for however long the new app takes to first fully repaint.
	s.altGrid = resizeGrid(nil, s.Cols, s.Rows)
	s.Grid, s.altGrid = s.altGrid, s.Grid
	s.usingAlt = true
	s.CursorX, s.CursorY, s.CurAttr = 0, 0, Attr{}
	// The images belong to the buffer that was just swapped out; the fresh
	// alternate buffer starts clean, otherwise the previous screen's sixels
	// would keep rendering over the full-screen app.
	s.Images = nil
}

// ExitAltScreen restores the primary screen buffer and the cursor
// position/attribute saved by EnterAltScreen (CSI ?1049l). A no-op if not
// currently on the alternate buffer.
func (s *Screen) ExitAltScreen() {
	if !s.usingAlt {
		return
	}
	s.Grid, s.altGrid = s.altGrid, s.Grid
	s.usingAlt = false
	s.CursorX, s.CursorY, s.CurAttr = s.altSaved.x, s.altSaved.y, s.altSaved.attr
	// The images drawn by the full-screen app are gone with its buffer.
	s.Images = nil
}

func newRow(cols int) []Cell {
	row := make([]Cell, cols)
	for x := range row {
		row[x] = blankCell()
	}
	return row
}

func (s *Screen) charset() Charset {
	if s.usingG1 {
		return s.g1
	}
	return s.g0
}

// Put writes r at the cursor using the current attribute and charset,
// then advances the cursor, wrapping/scrolling as VT340 autowrap dictates.
func (s *Screen) Put(r rune) {
	if s.pendingWrap {
		s.CursorX = 0
		s.lineFeed()
		s.pendingWrap = false
	}
	s.Grid[s.CursorY][s.CursorX] = Cell{Rune: translateRune(s.charset(), r), Attr: s.CurAttr}
	s.advanceCursor()
}

func (s *Screen) advanceCursor() {
	if s.CursorX < s.Cols-1 {
		s.CursorX++
		return
	}
	if s.AutoWrap {
		s.pendingWrap = true
	}
}

func (s *Screen) lineFeed() {
	if s.CursorY == s.ScrollBottom {
		s.ScrollUp(1)
		return
	}
	if s.CursorY < s.Rows-1 {
		s.CursorY++
	}
}

func (s *Screen) LineFeed() {
	s.pendingWrap = false
	s.lineFeed()
}

func (s *Screen) CarriageReturn() {
	s.pendingWrap = false
	s.CursorX = 0
}

// ScrollUp shifts the scroll region up by n lines, filling with blanks.
// Sixel images anchored inside the region move up with the text; an image
// whose anchor scrolls past the region's top has scrolled off and is
// dropped, so it doesn't linger overlaying newer content.
func (s *Screen) ScrollUp(n int) {
	top, bottom := s.ScrollTop, s.ScrollBottom
	for range n {
		copy(s.Grid[top:bottom], s.Grid[top+1:bottom+1])
		s.Grid[bottom] = newRow(s.Cols)
	}
	s.shiftImagesUp(n, top)
}

// ScrollDown shifts the scroll region down by n lines, filling with blanks.
// Images anchored inside the region move down with the text; an image whose
// anchor would drop below the region's bottom is dropped (see ScrollUp).
func (s *Screen) ScrollDown(n int) {
	top, bottom := s.ScrollTop, s.ScrollBottom
	for range n {
		copy(s.Grid[top+1:bottom+1], s.Grid[top:bottom])
		s.Grid[top] = newRow(s.Cols)
	}
	s.shiftImagesDown(n, top, bottom)
}

// shiftImagesUp moves image anchors that live inside the scrolled band up by
// n rows, dropping any that leave through the region's top edge. Anchors
// outside the band (in the protected area above the region) are untouched.
func (s *Screen) shiftImagesUp(n, top int) {
	if len(s.Images) == 0 {
		return
	}
	kept := s.Images[:0]
	for _, im := range s.Images {
		if im.Row < top {
			kept = append(kept, im)
			continue
		}
		im.Row -= n
		if im.Row >= top {
			kept = append(kept, im)
		}
	}
	s.Images = kept
}

// shiftImagesDown moves image anchors that live inside the scrolled band down
// by n rows, dropping any that leave through the region's bottom edge.
// Anchors outside the band are untouched.
func (s *Screen) shiftImagesDown(n, top, bottom int) {
	if len(s.Images) == 0 {
		return
	}
	kept := s.Images[:0]
	for _, im := range s.Images {
		if im.Row < top || im.Row > bottom {
			kept = append(kept, im)
			continue
		}
		im.Row += n
		if im.Row <= bottom {
			kept = append(kept, im)
		}
	}
	s.Images = kept
}

func (s *Screen) clampCursor() {
	top, bottom := 0, s.Rows-1
	if s.OriginMode {
		top, bottom = s.ScrollTop, s.ScrollBottom
	}
	s.CursorY = clamp(s.CursorY, top, bottom)
	s.CursorX = clamp(s.CursorX, 0, s.Cols-1)
}

// MoveTo positions the cursor, honoring origin mode's scroll-region-relative
// coordinates when active.
func (s *Screen) MoveTo(x, y int) {
	s.pendingWrap = false
	if s.OriginMode {
		y += s.ScrollTop
	}
	s.CursorX, s.CursorY = x, y
	s.clampCursor()
}

func (s *Screen) MoveBy(dx, dy int) {
	s.pendingWrap = false
	s.CursorX += dx
	s.CursorY += dy
	s.clampCursor()
}

func (s *Screen) SetScrollRegion(top, bottom int) {
	s.ScrollTop = clamp(top, 0, s.Rows-1)
	s.ScrollBottom = clamp(bottom, s.ScrollTop, s.Rows-1)
	s.MoveTo(0, 0)
}

func (s *Screen) DesignateG0(cs Charset) { s.g0 = cs }
func (s *Screen) DesignateG1(cs Charset) { s.g1 = cs }
func (s *Screen) ShiftOut()              { s.usingG1 = true }
func (s *Screen) ShiftIn()               { s.usingG1 = false }

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
