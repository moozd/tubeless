package render

import (
	"testing"

	"github.com/moozd/tubeless/pkg/screen"
)

func TestMergeScrollShiftsNetsSameRegion(t *testing.T) {
	events := []screen.ScrollShift{
		{Top: 0, Bottom: 23, Delta: 1},
		{Top: 0, Bottom: 23, Delta: 1},
	}
	merged := mergeScrollShifts(events)
	if len(merged) != 1 {
		t.Fatalf("len = %d, want 1", len(merged))
	}
	if merged[0].Delta != 2 {
		t.Fatalf("Delta = %d, want 2 (netted)", merged[0].Delta)
	}
}

func TestMergeScrollShiftsKeepsDistinctRegionsSeparate(t *testing.T) {
	events := []screen.ScrollShift{
		{Top: 0, Bottom: 23, Delta: 1},
		{Top: 5, Bottom: 10, Delta: -2},
	}
	merged := mergeScrollShifts(events)
	if len(merged) != 2 {
		t.Fatalf("len = %d, want 2 (different regions)", len(merged))
	}
	if merged[0].Delta != 1 || merged[1].Delta != -2 {
		t.Fatalf("merged = %+v, want deltas [1, -2]", merged)
	}
}

func TestApplyScrollEventsNoop(t *testing.T) {
	r := &Renderer{}
	r.ApplyScrollEvents(nil, 20)
	if r.ContentScrollActive() {
		t.Fatal("no events should not start a glide")
	}
}

func TestApplyScrollEventsStartsGlideFromNettedDelta(t *testing.T) {
	r := &Renderer{}
	events := []screen.ScrollShift{
		{Top: 0, Bottom: 23, Delta: 1},
		{Top: 0, Bottom: 23, Delta: 1},
	}
	r.ApplyScrollEvents(events, 20)
	if !r.ContentScrollActive() {
		t.Fatal("expected a content-scroll glide to start")
	}
	if r.shiftOffsetPx != 40 {
		t.Fatalf("shiftOffsetPx = %v, want 40 (netted delta 2 * cellH 20)", r.shiftOffsetPx)
	}
	if r.shiftTop != 0 || r.shiftBottom != 23 {
		t.Fatalf("shift band = [%d,%d], want [0,23]", r.shiftTop, r.shiftBottom)
	}
}

func TestApplyScrollEventsUsesLastDistinctRegion(t *testing.T) {
	r := &Renderer{}
	events := []screen.ScrollShift{
		{Top: 0, Bottom: 23, Delta: 1},
		{Top: 5, Bottom: 10, Delta: -3},
	}
	r.ApplyScrollEvents(events, 20)
	if r.shiftTop != 5 || r.shiftBottom != 10 {
		t.Fatalf("shift band = [%d,%d], want [5,10] (last region)", r.shiftTop, r.shiftBottom)
	}
	if r.shiftOffsetPx != -60 {
		t.Fatalf("shiftOffsetPx = %v, want -60", r.shiftOffsetPx)
	}
}

// Ground-truth recording itself (ScrollUp/ScrollDown/InsertLines/
// DeleteLines appending the right ScrollShift) is covered in
// pkg/screen's own tests, next to the mutators that produce it.
