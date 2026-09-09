package screen

import "testing"

func placedImage(row int) PlacedImage {
	return PlacedImage{Col: 0, Row: row}
}

func rowsOf(imgs []PlacedImage) []int {
	out := make([]int, 0, len(imgs))
	for _, im := range imgs {
		out = append(out, im.Row)
	}
	return out
}

func eqRows(a []int, b ...int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestScrollUpMovesImages(t *testing.T) {
	s := New(5, 5)
	s.Images = []PlacedImage{placedImage(1), placedImage(3), placedImage(4)}
	s.ScrollUp(1)
	if !eqRows(rowsOf(s.Images), 0, 2, 3) {
		t.Fatalf("after scroll up: rows %v, want [0 2 3]", rowsOf(s.Images))
	}
	// Image anchored at row 0 scrolls off through the top.
	s.ScrollUp(1)
	if !eqRows(rowsOf(s.Images), 1, 2) {
		t.Fatalf("after second scroll up: rows %v, want [1 2]", rowsOf(s.Images))
	}
}

func TestScrollUpProtectsAboveRegion(t *testing.T) {
	s := New(5, 5)
	s.SetScrollRegion(2, 4)
	s.Images = []PlacedImage{placedImage(0), placedImage(1), placedImage(2), placedImage(4)}
	s.ScrollUp(1)
	if !eqRows(rowsOf(s.Images), 0, 1, 3) {
		t.Fatalf("after scrolled region up: rows %v, want [0 1 3]", rowsOf(s.Images))
	}
}

func TestScrollDownDropsAtBottom(t *testing.T) {
	s := New(5, 5)
	s.Images = []PlacedImage{placedImage(1), placedImage(4)}
	s.ScrollDown(1)
	if !eqRows(rowsOf(s.Images), 2) {
		t.Fatalf("after scroll down: rows %v, want [2]", rowsOf(s.Images))
	}
}

func TestEraseAllClearsImages(t *testing.T) {
	s := New(5, 5)
	s.CursorY = 2
	s.Images = []PlacedImage{placedImage(0), placedImage(3)}
	s.EraseInDisplay(EraseAll)
	if len(s.Images) != 0 {
		t.Fatalf("EraseInDisplay(EraseAll) left %d images", len(s.Images))
	}
}

func TestErasePartialKeepsOutsideImages(t *testing.T) {
	s := New(5, 5)
	s.CursorY = 2
	s.Images = []PlacedImage{placedImage(0), placedImage(3)}
	s.EraseInDisplay(EraseToStart)
	if !eqRows(rowsOf(s.Images), 3) {
		t.Fatalf("erase-to-start kept %v, want [3]", rowsOf(s.Images))
	}
}

func TestAltScreenClearsImages(t *testing.T) {
	s := New(5, 5)
	s.Images = []PlacedImage{placedImage(0)}
	s.EnterAltScreen()
	if len(s.Images) != 0 {
		t.Fatal("EnterAltScreen left images behind")
	}
	s.Images = []PlacedImage{placedImage(1)}
	s.ExitAltScreen()
	if len(s.Images) != 0 {
		t.Fatal("ExitAltScreen left images behind")
	}
}

func TestResetClearsImages(t *testing.T) {
	s := New(5, 5)
	s.Images = []PlacedImage{placedImage(0)}
	s.Reset()
	if len(s.Images) != 0 {
		t.Fatal("Reset left images behind")
	}
}
