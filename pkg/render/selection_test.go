package render

import "testing"

func TestSelectionContainsSingleRow(t *testing.T) {
	sel := Selection{Active: true, StartX: 2, StartY: 1, EndX: 5, EndY: 1}
	cases := map[[2]int]bool{
		{1, 1}: false,
		{2, 1}: true,
		{5, 1}: true,
		{6, 1}: false,
		{3, 0}: false,
	}
	for pos, want := range cases {
		if got := sel.Contains(pos[0], pos[1]); got != want {
			t.Errorf("Contains(%d,%d) = %v, want %v", pos[0], pos[1], got, want)
		}
	}
}

func TestSelectionContainsMultiRowStreamSemantics(t *testing.T) {
	sel := Selection{Active: true, StartX: 5, StartY: 2, EndX: 3, EndY: 4}
	// First row: only from StartX onward. Middle row: fully selected.
	// Last row: only up to EndX.
	if sel.Contains(4, 2) {
		t.Error("Contains(4,2) = true, want false (before start col on start row)")
	}
	if !sel.Contains(5, 2) || !sel.Contains(50, 2) {
		t.Error("start row should be selected from StartX onward")
	}
	if !sel.Contains(0, 3) || !sel.Contains(50, 3) {
		t.Error("middle row should be fully selected")
	}
	if !sel.Contains(3, 4) || sel.Contains(4, 4) {
		t.Error("last row should be selected only up to EndX")
	}
}

func TestSelectionInactiveContainsNothing(t *testing.T) {
	sel := Selection{Active: false, StartX: 0, StartY: 0, EndX: 10, EndY: 10}
	if sel.Contains(5, 5) {
		t.Error("inactive selection should contain nothing")
	}
}
