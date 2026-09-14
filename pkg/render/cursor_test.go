package render

import "testing"

// TestUpdateCursorTypingStaysBelowMorphThreshold exercises UpdateCursor's
// speed/morph tracking without a GL context: advancing one cell per
// retarget — typing, or a held arrow key repeating at a brisk 50cps —
// should never cross cursorMorphSpeedLow, so ordinary text entry stays
// visually consistent instead of flickering into a ball.
func TestUpdateCursorTypingStaysBelowMorphThreshold(t *testing.T) {
	r := &Renderer{}
	const dt = 0.02 // 20ms retarget interval, i.e. a fast 50cps key-repeat
	col := 0

	for i := 0; i < 60; i++ {
		col++
		r.UpdateCursor(col, 0, true, dt)
		if r.cursorMorph > 0.05 {
			t.Fatalf("cursorMorph = %v after %d single-cell steps, want <= 0.05 (typing shouldn't morph)", r.cursorMorph, i+1)
		}
	}
}

// TestUpdateCursorBigJumpRampsMorphUpAndDown mirrors a Neovim-style jump
// (:, gg, G, a search result) landing many cells away in a single frame:
// there is no distance-based snap any more (see UpdateCursor's doc
// comment), so the glide's own speed spikes hard for a frame or two,
// which should ramp cursorMorph up toward the ball/tail shape; once the
// glide catches up and settles, cursorMorph should ease back down to the
// plain block.
func TestUpdateCursorBigJumpRampsMorphUpAndDown(t *testing.T) {
	r := &Renderer{}
	const dt = 1.0 / 60.0
	r.UpdateCursor(0, 0, true, dt)

	for i := 0; i < 5; i++ {
		r.UpdateCursor(30, 0, true, dt)
	}
	if r.cursorMorph < 0.5 {
		t.Fatalf("cursorMorph = %v shortly after a 30-cell jump, want >= 0.5", r.cursorMorph)
	}

	for i := 0; i < 90; i++ {
		r.UpdateCursor(30, 0, true, dt)
	}
	if r.cursorMorph > 0.05 {
		t.Fatalf("cursorMorph = %v after the glide settled, want <= 0.05", r.cursorMorph)
	}
}

// TestSmoothstep32 checks the GLSL smoothstep port's boundary and midpoint
// behavior — it drives the speed-to-morph mapping, so an off-by-one edge
// here would misfire the ball/tail threshold silently.
func TestSmoothstep32(t *testing.T) {
	if got := smoothstep32(10, 20, 5); got != 0 {
		t.Errorf("below edge0 = %v, want 0", got)
	}
	if got := smoothstep32(10, 20, 25); got != 1 {
		t.Errorf("above edge1 = %v, want 1", got)
	}
	if got := smoothstep32(10, 20, 15); got != 0.5 {
		t.Errorf("at midpoint = %v, want 0.5", got)
	}
}
