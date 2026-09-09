package render

// Selection is a text-flow (not rectangular) mouse selection range,
// between two grid cells. It's owned by cmd/tubeless (mutated by mouse
// drag events, which arrive on the render/GL thread) rather than
// screen.Screen: Screen has a strict single-writer-goroutine contract
// (the PTY-input coordinator — see its own doc comment), which a second
// mutator would violate, and selection is a local UI concept the PTY
// stream has no say in anyway — the same "overlay, don't bake into the
// grid" treatment the cursor already gets (see Renderer's cursorCol/Row).
type Selection struct {
	Active bool
	// StartX/Y is where the drag began, EndX/Y is the current/final
	// point. Order (which came first in reading order) is resolved by
	// Normalized, not assumed here, since a drag can go either direction.
	StartX, StartY, EndX, EndY int
}

// Normalized returns the selection's two endpoints in reading order
// (earlier row/col first).
func (s Selection) Normalized() (x0, y0, x1, y1 int) {
	x0, y0, x1, y1 = s.StartX, s.StartY, s.EndX, s.EndY
	if y0 > y1 || (y0 == y1 && x0 > x1) {
		return x1, y1, x0, y0
	}
	return x0, y0, x1, y1
}

// Contains reports whether (x,y) lies within the selection, in normal
// (stream/text-flow, not block/rectangular) semantics: the first selected
// row runs from its start column to the row's end, the last selected row
// runs from column 0 to its end column, and any rows strictly between run
// in full.
func (s Selection) Contains(x, y int) bool {
	if !s.Active {
		return false
	}
	x0, y0, x1, y1 := s.Normalized()
	switch {
	case y < y0 || y > y1:
		return false
	case y0 == y1:
		return x >= x0 && x <= x1
	case y == y0:
		return x >= x0
	case y == y1:
		return x <= x1
	default:
		return true
	}
}
