package render

import "testing"

// rectCount is how many rect instances appendFillRects emitted.
func rectCount(e edgeCont) int {
	cell := cellRect{w: 1, h: 1}
	out := appendFillRects(nil, cell, 10, 20, e, [3]float32{1, 1, 1})
	return len(out) / rectInstanceFloats
}

// A page cell below and left of a highlighted span continues up and
// right, but its up-right diagonal is the span: one grown rect would
// paint the span's bottom-left corner with the page color.
func TestFillRectsSplitAtForeignCorner(t *testing.T) {
	e := edgeCont{Up: true, Right: true, Left: true, Down: true,
		UpLeft: true, DownRight: true, DownLeft: true}
	if got := rectCount(e); got != 2 {
		t.Fatalf("rects = %d, want 2 (split at the foreign corner)", got)
	}
}

func TestFillRectsSplitRectsSkipDiagonal(t *testing.T) {
	e := edgeCont{Up: true, Right: true}
	out := appendFillRects(nil, cellRect{w: 1, h: 1}, 10, 20, e, [3]float32{})
	for i := 0; i < len(out); i += rectInstanceFloats {
		ry, rw, rh := out[i+3], out[i+4], out[i+5]
		isGrownRight := rw > 1
		isGrownUp := ry < 0 || rh > 1
		if isGrownRight && isGrownUp {
			t.Fatalf("rect %d grows right and up, so it covers the diagonal", i/rectInstanceFloats)
		}
	}
}

func TestFillRectsSingleRectInSameFill(t *testing.T) {
	all := edgeCont{Up: true, Right: true, Down: true, Left: true,
		UpLeft: true, UpRight: true, DownRight: true, DownLeft: true}
	if got := rectCount(all); got != 1 {
		t.Fatalf("rects = %d, want 1 inside a uniform fill", got)
	}
}

// One continuing axis never reaches a diagonal cell.
func TestFillRectsSingleRectOnOneAxis(t *testing.T) {
	if got := rectCount(edgeCont{Left: true, Right: true}); got != 1 {
		t.Fatalf("rects = %d, want 1", got)
	}
}

func TestCoversCorner(t *testing.T) {
	cases := []struct {
		name           string
		x0, y0, x1, y1 float32
		dx, dy         int
		want           bool
	}{
		{"full block up-right", 0, 0, 1, 1, 1, -1, true},
		{"lower half up-right", 0, 0.5, 1, 1, 1, -1, true},
		{"upper half up-right", 0, 0, 1, 0.5, 1, -1, false},
		{"right half up-right", 0.5, 0, 1, 1, 1, -1, false},
		{"left half down-left", 0, 0, 0.5, 1, -1, 1, false},
		{"right half down-left", 0.5, 0, 1, 1, -1, 1, true},
	}
	for _, c := range cases {
		got := coversCorner(c.x0, c.y0, c.x1, c.y1, c.dx, c.dy)
		if got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
