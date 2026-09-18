package render

import (
	"testing"

	"github.com/moozd/tubeless/pkg/config"
)

var testTrail = config.Trail{Enabled: true, Size: 0.5, Length: 0.13}

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
		r.UpdateCursor(col, 0, true, dt, testTrail)
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
	r.UpdateCursor(0, 0, true, dt, testTrail)

	for i := 0; i < 5; i++ {
		r.UpdateCursor(30, 0, true, dt, testTrail)
	}
	if r.cursorMorph < 0.45 {
		t.Fatalf("cursorMorph = %v shortly after a 30-cell jump, want >= 0.45", r.cursorMorph)
	}

	for i := 0; i < 90; i++ {
		r.UpdateCursor(30, 0, true, dt, testTrail)
	}
	if r.cursorMorph > 0.05 {
		t.Fatalf("cursorMorph = %v after the glide settled, want <= 0.05", r.cursorMorph)
	}
}

// TestCursorBrightnessStyles checks each BlinkStyle's shape: "static" stays
// pinned at full brightness regardless of phase, "hard" toggles cleanly at
// the half-period boundary, and "ease" breathes between the 0.35 floor and
// 1.0 ceiling without ever going fully dark while visible.
func TestCursorBrightnessStyles(t *testing.T) {
	const period = 1.0

	if got := cursorBrightness("static", 0.9, period, true); got != 1 {
		t.Errorf("static @ phase 0.9 = %v, want 1", got)
	}
	if got := cursorBrightness("hard", 0.1, period, true); got != 1 {
		t.Errorf("hard @ phase 0.1 (first half) = %v, want 1", got)
	}
	if got := cursorBrightness("hard", 0.6, period, true); got != 0 {
		t.Errorf("hard @ phase 0.6 (second half) = %v, want 0", got)
	}
	if got := cursorBrightness("ease", 0, period, true); got < 0.34 || got > 1 {
		t.Errorf("ease @ phase 0 = %v, want within [0.34, 1]", got)
	}
	if got := cursorBrightness("ease", period/4, period, true); got != 1 {
		t.Errorf("ease @ quarter phase (sine peak) = %v, want 1", got)
	}
}

// TestCursorBrightnessHiddenAndUnset covers the two overrides every style
// shares: an invisible cursor is always black, and a zero/negative
// PulsePeriod (an unset pulse) reads as static even for "ease"/"hard".
func TestCursorBrightnessHiddenAndUnset(t *testing.T) {
	if got := cursorBrightness("ease", 0.5, 1.0, false); got != 0 {
		t.Errorf("hidden cursor = %v, want 0", got)
	}
	if got := cursorBrightness("ease", 0.5, 0, true); got != 1 {
		t.Errorf("ease with period <= 0 = %v, want 1 (reads as static)", got)
	}
	if got := cursorBrightness("hard", 0.5, 0, true); got != 1 {
		t.Errorf("hard with period <= 0 = %v, want 1 (reads as static)", got)
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
