package render

import (
	"testing"

	"github.com/moozd/tubeless/pkg/screen"
)

func TestApplyDetectedRowShiftStartsGlide(t *testing.T) {
	r := &Renderer{}
	r.ApplyDetectedRowShift(screen.RowShift{Top: 0, Bottom: 23, Left: 0, Right: 79, Delta: 2}, 20)
	if !r.ContentScrollActive() {
		t.Fatal("expected a content-scroll glide to start")
	}
	if r.shiftOffsetPx != 40 {
		t.Fatalf("shiftOffsetPx = %v, want 40 (delta 2 * cellH 20)", r.shiftOffsetPx)
	}
	if r.shiftTop != 0 || r.shiftBottom != 23 {
		t.Fatalf("shift band = [%d,%d], want [0,23]", r.shiftTop, r.shiftBottom)
	}
	if r.shiftColLeft != 0 || r.shiftColRight != 79 {
		t.Fatalf("shift column bounds = [%d,%d], want [0,79]", r.shiftColLeft, r.shiftColRight)
	}
}

// TestApplyDetectedRowShiftConfinesColumns is the renderer-side half of
// the split-pane fix: a shift screen.DetectContentShift found within one
// column band (see columnBands) must confine the glide to that band —
// otherwise a frozen neighboring pane sharing the same physical rows
// would visibly slide along with the pane that actually scrolled.
func TestApplyDetectedRowShiftConfinesColumns(t *testing.T) {
	r := &Renderer{}
	r.ApplyDetectedRowShift(screen.RowShift{Top: 0, Bottom: 9, Left: 19, Right: 39, Delta: 2}, 20)
	if r.shiftColLeft != 19 || r.shiftColRight != 39 {
		t.Fatalf("shift column bounds = [%d,%d], want [19,39] (confined to the scrolled pane)", r.shiftColLeft, r.shiftColRight)
	}
}

func TestApplyDetectedColShiftStartsGlide(t *testing.T) {
	r := &Renderer{}
	r.ApplyDetectedColShift(screen.ColShift{Left: 5, Right: 10, Top: 0, Bottom: 23, Delta: -3}, 20)
	if !r.ContentScrollColsActive() {
		t.Fatal("expected a horizontal content-scroll glide to start")
	}
	if r.shiftOffsetPxX != -60 {
		t.Fatalf("shiftOffsetPxX = %v, want -60 (delta -3 * cellW 20)", r.shiftOffsetPxX)
	}
	if r.shiftLeft != 5 || r.shiftRight != 10 {
		t.Fatalf("shift band = [%d,%d], want [5,10]", r.shiftLeft, r.shiftRight)
	}
}

// Detection itself (screen.DetectContentShift/DetectHorizontalContentShift)
// is covered in pkg/screen's own tests, next to the Grid it diffs.

func TestBeginContentScrollAccumulatesSameRegion(t *testing.T) {
	r := &Renderer{}
	r.BeginContentScroll(0, 23, 0, 79, 20)
	r.BeginContentScroll(0, 23, 0, 79, 20)
	if r.shiftOffsetPx != 40 {
		t.Fatalf("shiftOffsetPx = %v, want 40 (accumulated across two calls)", r.shiftOffsetPx)
	}
	if !r.ContentScrollActive() {
		t.Fatal("expected glide to still be active")
	}
}

func TestBeginContentScrollResetsOnDifferentRegion(t *testing.T) {
	r := &Renderer{}
	r.BeginContentScroll(0, 23, 0, 79, 20)
	r.BeginContentScroll(5, 10, 0, 79, 30)
	if r.shiftOffsetPx != 30 {
		t.Fatalf("shiftOffsetPx = %v, want 30 (reset, not accumulated, on region change)", r.shiftOffsetPx)
	}
	if r.shiftTop != 5 || r.shiftBottom != 10 {
		t.Fatalf("shift band = [%d,%d], want [5,10]", r.shiftTop, r.shiftBottom)
	}
}

// TestBeginContentScrollResetsOnDifferentColumnBand guards the new
// axis: a same row band but a DIFFERENT column band (one split pane
// scrolling, then a different pane scrolling) must reset, not
// accumulate — they're unrelated glides that happen to share row bounds.
func TestBeginContentScrollResetsOnDifferentColumnBand(t *testing.T) {
	r := &Renderer{}
	r.BeginContentScroll(0, 23, 0, 39, 20)
	r.BeginContentScroll(0, 23, 40, 79, 30)
	if r.shiftOffsetPx != 30 {
		t.Fatalf("shiftOffsetPx = %v, want 30 (reset, not accumulated, on column-band change)", r.shiftOffsetPx)
	}
	if r.shiftColLeft != 40 || r.shiftColRight != 79 {
		t.Fatalf("shift column bounds = [%d,%d], want [40,79]", r.shiftColLeft, r.shiftColRight)
	}
}

func TestBeginContentScrollStartsFreshOnceSettled(t *testing.T) {
	r := &Renderer{}
	r.BeginContentScroll(0, 23, 0, 79, 20)
	r.UpdateContentScroll(10) // huge dt settles it (exponential decay below the 0.3px threshold)
	if r.ContentScrollActive() {
		t.Fatal("expected glide to have settled")
	}
	r.BeginContentScroll(0, 23, 0, 79, 20)
	if r.shiftOffsetPx != 20 {
		t.Fatalf("shiftOffsetPx = %v, want 20 (fresh start, not accumulated onto a settled glide)", r.shiftOffsetPx)
	}
}

func TestBeginContentScrollColsAccumulatesSameRegion(t *testing.T) {
	r := &Renderer{}
	r.BeginContentScrollCols(0, 79, 0, 23, 20)
	r.BeginContentScrollCols(0, 79, 0, 23, 20)
	if r.shiftOffsetPxX != 40 {
		t.Fatalf("shiftOffsetPxX = %v, want 40 (accumulated across two calls)", r.shiftOffsetPxX)
	}
	if !r.ContentScrollColsActive() {
		t.Fatal("expected glide to still be active")
	}
}

func TestBeginContentScrollColsResetsOnDifferentRegion(t *testing.T) {
	r := &Renderer{}
	r.BeginContentScrollCols(0, 79, 0, 23, 20)
	r.BeginContentScrollCols(5, 10, 0, 23, 30)
	if r.shiftOffsetPxX != 30 {
		t.Fatalf("shiftOffsetPxX = %v, want 30 (reset, not accumulated, on region change)", r.shiftOffsetPxX)
	}
	if r.shiftLeft != 5 || r.shiftRight != 10 {
		t.Fatalf("shift band = [%d,%d], want [5,10]", r.shiftLeft, r.shiftRight)
	}
}

func TestBeginContentScrollColsStartsFreshOnceSettled(t *testing.T) {
	r := &Renderer{}
	r.BeginContentScrollCols(0, 79, 0, 23, 20)
	r.UpdateContentScrollCols(10) // huge dt settles it (exponential decay below the 0.3px threshold)
	if r.ContentScrollColsActive() {
		t.Fatal("expected glide to have settled")
	}
	r.BeginContentScrollCols(0, 79, 0, 23, 20)
	if r.shiftOffsetPxX != 20 {
		t.Fatalf("shiftOffsetPxX = %v, want 20 (fresh start, not accumulated onto a settled glide)", r.shiftOffsetPxX)
	}
}
