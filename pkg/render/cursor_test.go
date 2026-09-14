package render

import "testing"

// TestUpdateCursorMorphRampsUpAndDown exercises UpdateCursor's speed/morph
// tracking without a GL context: a held-arrow-key repeat (one cell every
// 30ms, well under cursorSnapDist) should ramp cursorMorph up toward the
// ball/tail shape, and letting the cursor sit still should ease it back
// down to the plain block.
func TestUpdateCursorMorphRampsUpAndDown(t *testing.T) {
	r := &Renderer{}
	const dt = 0.03 // 30ms, a typical key-repeat interval
	col := 0

	for i := 0; i < 40; i++ {
		col++
		r.UpdateCursor(col, 0, true, dt)
	}
	if r.cursorMorph < 0.5 {
		t.Fatalf("cursorMorph = %v after sustained fast movement, want >= 0.5", r.cursorMorph)
	}

	for i := 0; i < 60; i++ {
		r.UpdateCursor(col, 0, true, dt)
	}
	if r.cursorMorph > 0.05 {
		t.Fatalf("cursorMorph = %v after the cursor settled, want <= 0.05", r.cursorMorph)
	}
}

// TestUpdateCursorSnapResetsMorph mirrors UpdateCursor's own reasoning: a
// jump bigger than cursorSnapDist isn't organic motion for the ball/tail
// effect to react to, so it should reset speed/morph to 0 immediately
// rather than reading as a burst of speed.
func TestUpdateCursorSnapResetsMorph(t *testing.T) {
	r := &Renderer{}
	r.UpdateCursor(0, 0, true, 0.03)
	for i := 0; i < 40; i++ {
		r.UpdateCursor(i+1, 0, true, 0.03)
	}
	if r.cursorMorph <= 0 {
		t.Fatalf("cursorMorph = %v before the jump, want > 0 to make this a meaningful test", r.cursorMorph)
	}

	r.UpdateCursor(200, 50, true, 0.03)
	if r.cursorMorph != 0 || r.cursorSpeed != 0 {
		t.Fatalf("after a snap: cursorMorph = %v, cursorSpeed = %v, want both 0", r.cursorMorph, r.cursorSpeed)
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
