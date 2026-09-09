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

	// scrollback holds rows that scrolled off the primary screen's actual
	// top edge (see ScrollUp). It's a pointer so Clone() — called on
	// every PTY-output chunk — can copy it in O(1) instead of deep-copying
	// up to scrollbackCap rows every time; see appendScrollback.
	scrollback    *scrollbackBuf
	scrollbackCap int

	// Mouse-reporting state, set by CSI ?1000/1002/1003/1006h/l (see
	// csi.go's setMode) — a child app (vim, tmux, htop) that wants mouse
	// events sets these; cmd/tubeless's mouse handling checks MouseMode
	// before treating a click/drag/wheel as local selection/scroll rather
	// than something to forward to the PTY. See mouse.go for the SGR
	// encoder that builds the actual escape sequence.
	MouseMode      MouseMode
	MouseSGR       bool
	BracketedPaste bool

	// ApplicationCursorKeys is DECCKM (CSI ?1h/l): while set, the arrow
	// keys and Home/End should be sent as SS3 (ESC O <letter>) instead of
	// CSI (ESC [ <letter>) when unmodified — vim, less, and most other
	// full-screen apps turn this on so they can tell cursor keys apart
	// from a plain CSI sequence. See cmd/tubeless/input.go's encodeCursorKey.
	ApplicationCursorKeys bool
}

// MouseMode is which mouse events (if any) the application has asked to
// receive, via the corresponding DEC private mode.
type MouseMode int

const (
	MouseOff   MouseMode = iota
	MouseClick           // ?1000: button press/release only
	MouseDrag            // ?1002: press/release + motion while a button is held
	MouseAny             // ?1003: press/release + all motion, button held or not
)

// scrollbackBuf is scrollback's actual storage, wrapped in its own type so
// Screen.Clone() can copy the pointer alone: a Screen's mutator (the sole
// writer, appendScrollback) always replaces this pointer with a new value
// rather than mutating rows in place, so a previously-published clone's
// view of a *scrollbackBuf it already holds never changes underneath it —
// same "publish immutable snapshots" contract Screen itself follows.
type scrollbackBuf struct {
	rows [][]Cell
	// start is the index of the first live row: rows[start:] is the actual
	// scrollback content, rows[:start] is dead weight not yet reclaimed.
	// appendScrollback bumps start instead of copying on most calls once at
	// cap, only paying the O(cap) compaction cost once every cap appends —
	// see appendScrollback.
	start int
}

type savedCursor struct {
	x, y int
	attr Attr
}

// DefaultScrollbackLines is the scrollback cap a new Screen starts with
// before SetScrollbackCap (driven by config.Config's Scrollback.Lines)
// applies the user's configured value.
const DefaultScrollbackLines = 5000

func New(cols, rows int) *Screen {
	s := &Screen{Cols: cols, Rows: rows, AutoWrap: true, CursorVisible: true}
	s.ScrollBottom = rows - 1
	s.Grid = make([][]Cell, rows)
	for y := range s.Grid {
		s.Grid[y] = newRow(cols)
	}
	s.scrollback = &scrollbackBuf{}
	s.scrollbackCap = DefaultScrollbackLines
	return s
}

// SetScrollbackCap changes how many rows scrolled off the top are
// retained, trimming immediately if the new cap is smaller. Replaces the
// scrollback pointer rather than mutating the existing *scrollbackBuf in
// place, so a previously-published clone's view is unaffected.
func (s *Screen) SetScrollbackCap(n int) {
	if n < 0 {
		n = 0
	}
	s.scrollbackCap = n
	live := s.scrollback.rows[s.scrollback.start:]
	if len(live) > n {
		live = live[len(live)-n:]
	}
	s.scrollback = &scrollbackBuf{rows: append([][]Cell(nil), live...)}
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
	// scrollback is copy-on-write (see its doc comment): the struct copy
	// above already carries the pointer over, and appendScrollback never
	// mutates a *scrollbackBuf a clone might be holding, only replaces
	// s.scrollback with a new one — so no deep copy is needed here, unlike
	// Grid/Images above. This is what keeps Clone() (called on every
	// PTY-output chunk) cheap even with thousands of scrollback rows.
	return &c
}

// Reset restores a freshly-initialized state at the current size (RIS).
func (s *Screen) Reset() {
	cols, rows, scrollbackCap := s.Cols, s.Rows, s.scrollbackCap
	*s = Screen{Cols: cols, Rows: rows, AutoWrap: true, CursorVisible: true}
	s.ScrollBottom = rows - 1
	s.Grid = make([][]Cell, rows)
	for y := range s.Grid {
		s.Grid[y] = newRow(cols)
	}
	s.scrollback = &scrollbackBuf{}
	s.scrollbackCap = scrollbackCap
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

// blankRow is EraseInDisplay's row-at-a-time equivalent of erasedCell: a
// whole row erased to the cursor's current background, not the hardcoded
// default newRow uses for structural allocation (Reset/Resize/initial
// grid) where there's no cursor attribute to erase "to" in the first
// place.
func (s *Screen) blankRow() []Cell {
	row := make([]Cell, s.Cols)
	for x := range row {
		row[x] = erasedCell(s.CurAttr)
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
//
// A row leaving through the top is captured to scrollback first, but only
// when the scroll region IS the whole screen (top==0, bottom==Rows-1) and
// we're on the primary buffer: a partial DEC scroll region (some apps use
// one to pin a status line while scrolling the rest) isn't ordinary
// "new output pushed the old line up" scrolling, so its content was never
// meant to become history, and an alt-screen app's (vim, htop) redraws
// must never spam scrollback.
func (s *Screen) ScrollUp(n int) {
	top, bottom := s.ScrollTop, s.ScrollBottom
	n = min(n, bottom-top+1)
	capture := !s.usingAlt && top == 0 && bottom == s.Rows-1
	if capture {
		for y := top; y < top+n; y++ {
			s.appendScrollback(s.Grid[y])
		}
	}
	copy(s.Grid[top:bottom-n+1], s.Grid[top+n:bottom+1])
	for y := bottom - n + 1; y <= bottom; y++ {
		s.Grid[y] = newRow(s.Cols)
	}
	s.shiftImagesUp(n, top)
}

// appendScrollback adds row (copied — the caller's slice is about to be
// overwritten) to scrollback, evicting from the front once scrollbackCap
// is exceeded. Always builds a new *scrollbackBuf rather than mutating the
// existing one in place — see scrollback's doc comment.
func (s *Screen) appendScrollback(row []Cell) {
	if s.scrollbackCap <= 0 {
		return
	}
	rowCopy := append([]Cell(nil), row...)
	rows := append(s.scrollback.rows, rowCopy)
	start := s.scrollback.start
	if len(rows)-start > s.scrollbackCap {
		start++
	}
	if start >= s.scrollbackCap {
		// The dead prefix has grown to a full cap's worth: copy down into a
		// fresh, exactly-sized backing array so those rows' cells become
		// reclaimable, rather than reslicing which would keep every row
		// ever pushed reachable through the still-growing backing array
		// underneath — a real memory leak for a long-running session that
		// scrolls well past the cap. Doing this once every scrollbackCap
		// appends (instead of on every append) is what makes eviction
		// amortized O(1) rather than O(scrollbackCap) per scrolled line.
		trimmed := make([][]Cell, s.scrollbackCap)
		copy(trimmed, rows[start:])
		rows, start = trimmed, 0
	}
	s.scrollback = &scrollbackBuf{rows: rows, start: start}
}

// ScrollbackLen is how many rows are currently retained above the primary
// screen's live top edge.
func (s *Screen) ScrollbackLen() int {
	return len(s.scrollback.rows) - s.scrollback.start
}

// InAltScreen reports whether the alternate screen buffer (a full-screen
// app like vim/htop/less) is currently active — callers use this to
// disable scrollback viewing (there's nothing meaningful to scroll back
// through under a full-screen app's own redraws) rather than reaching
// into the unexported usingAlt field directly.
func (s *Screen) InAltScreen() bool {
	return s.usingAlt
}

// VisibleWindow returns the `rows`-tall window of content that should be
// on screen when scrolled back by scrollOffset lines from the live tail
// (0 = the normal, unscrolled view — Grid itself). scrollOffset is
// clamped to [0, ScrollbackLen()]. The returned rows are shared with
// scrollback/Grid's own backing storage — treat them read-only, same as
// Grid is expected to be once published via Clone().
func (s *Screen) VisibleWindow(scrollOffset int) [][]Cell {
	if scrollOffset <= 0 {
		return s.Grid
	}
	sb := s.scrollback.rows[s.scrollback.start:]
	if scrollOffset > len(sb) {
		scrollOffset = len(sb)
	}
	window := make([][]Cell, s.Rows)
	// The window's bottom edge sits scrollOffset rows above the live
	// tail: rows from scrollback fill from the top, then Grid fills
	// whatever's left once scrollback is exhausted going down.
	for i := range window {
		src := len(sb) - scrollOffset + i
		switch {
		case src < 0:
			window[i] = newRow(s.Cols)
		case src < len(sb):
			window[i] = sb[src]
		default:
			window[i] = s.Grid[src-len(sb)]
		}
	}
	return window
}

// ScrollDown shifts the scroll region down by n lines, filling with blanks.
// Images anchored inside the region move down with the text; an image whose
// anchor would drop below the region's bottom is dropped (see ScrollUp).
func (s *Screen) ScrollDown(n int) {
	top, bottom := s.ScrollTop, s.ScrollBottom
	n = min(n, bottom-top+1)
	copy(s.Grid[top+n:bottom+1], s.Grid[top:bottom-n+1])
	for y := top; y < top+n; y++ {
		s.Grid[y] = newRow(s.Cols)
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
