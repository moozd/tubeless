package main

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/moozd/tubeless/pkg/config"
	"github.com/moozd/tubeless/pkg/ptyio"
	"github.com/moozd/tubeless/pkg/screen"
)

// TestPtyCoordinatorSetsCloseRequestedOnShellExit locks in the fix for the
// zombie-process bug: when the shell exits, ptyCoordinator must set
// closeRequested so runLoop actually exits and the deferred sess.Close()
// (which reaps the shell) runs, instead of the window sitting open with the
// shell left unreaped indefinitely.
func TestPtyCoordinatorSetsCloseRequestedOnShellExit(t *testing.T) {
	sess, err := ptyio.Start("/bin/sh", []string{"-c", "exit 0"}, cols, rows)
	if err != nil {
		t.Fatalf("start shell: %v", err)
	}
	defer sess.Close()

	var shared atomic.Pointer[screen.Screen]
	shared.Store(screen.New(cols, rows))
	resizeCh := make(chan resizeReq, 1)
	closeRequested := new(atomic.Bool)

	done := make(chan struct{})
	go func() {
		ptyCoordinator(sess, &shared, resizeCh, 0, closeRequested)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ptyCoordinator did not return after the shell exited")
	}
	if !closeRequested.Load() {
		t.Fatal("closeRequested was not set after the shell exited")
	}
}

// TestEffectiveAtlasScale guards the fix for text looking sharp on a
// Retina display but soft on a standard one at the same atlas.scale:
// the raster resolution actually used must scale with the display's own
// DPI, so a given config value implies the same mipmap minification
// depth (and so the same look) on every display, not the same raw
// raster multiplier.
func TestEffectiveAtlasScale(t *testing.T) {
	cases := []struct {
		scale int
		dpi   float32
		want  int
	}{
		{scale: 3, dpi: 1, want: 3},
		{scale: 3, dpi: 2, want: 6},
		{scale: 6, dpi: 2, want: 12},
		{scale: 2, dpi: 1.5, want: 3},
		{scale: 3, dpi: 0, want: 3},  // unreported scale treated as 1x
		{scale: 3, dpi: -1, want: 3}, // never negative/zero either
	}
	for _, c := range cases {
		if got := effectiveAtlasScale(c.scale, c.dpi); got != c.want {
			t.Errorf("effectiveAtlasScale(%d, %v) = %d, want %d", c.scale, c.dpi, got, c.want)
		}
	}
}

// TestAutoAtlasScale guards the fix for atlas.scale "auto" (Scale <= 0)
// picking a base that looks right on a standard-DPI display too: naively
// reusing the Retina-tuned default of 4 there makes the mip chain deeper
// than it needs to be, compounding coverage loss on thin strokes instead
// of helping (see effectiveAtlasScale's doc comment).
func TestAutoAtlasScale(t *testing.T) {
	cases := []struct {
		dpi  float32
		want int
	}{
		{dpi: 1, want: 1},
		{dpi: 0, want: 1},
		{dpi: 1.5, want: 2},
		{dpi: 2, want: 4},
		{dpi: 3, want: 4},
	}
	for _, c := range cases {
		if got := autoAtlasScale(c.dpi); got != c.want {
			t.Errorf("autoAtlasScale(%v) = %d, want %d", c.dpi, got, c.want)
		}
	}
}

// TestBuildFacesForResolvesAutoScale guards "auto" actually taking effect
// end to end: with Scale <= 0, buildFacesFor must resolve a per-DPI base
// via autoAtlasScale rather than treating the sentinel as a literal raster
// multiplier (which would collapse every display to effectiveScale 0).
func TestBuildFacesForResolvesAutoScale(t *testing.T) {
	const maxTextureSize = 16384
	autoCfg := config.Config{
		Font:  config.Font{Size: 14, LineHeight: 1},
		Atlas: config.Atlas{Scale: 0, Gamma: 1.0},
	}
	explicitCfg := autoCfg
	explicitCfg.Atlas.Scale = autoAtlasScale(2)

	_, autoScale, err := buildFacesFor(autoCfg, 2, maxTextureSize)
	if err != nil {
		t.Fatalf("buildFacesFor(auto): %v", err)
	}
	_, explicitScale, err := buildFacesFor(explicitCfg, 2, maxTextureSize)
	if err != nil {
		t.Fatalf("buildFacesFor(explicit base %d): %v", explicitCfg.Atlas.Scale, err)
	}
	if autoScale != explicitScale {
		t.Fatalf("auto resolved to effectiveScale %d, but explicit base %d (what autoAtlasScale(2) returns) resolved to %d",
			autoScale, explicitCfg.Atlas.Scale, explicitScale)
	}
}

// TestZoomedFontSize guards the fix for a session's live font zoom
// surviving an unrelated config-file reload: runLoop reapplies
// fontZoomSteps onto the freshly resolved base size via this same
// function rather than discarding it (see runLoop's watch.changed
// branch), so the formula and its clamp must be exact.
func TestZoomedFontSize(t *testing.T) {
	cases := []struct {
		base, steps, want int
	}{
		{base: 14, steps: 0, want: 14},
		{base: 14, steps: 3, want: 20},
		{base: 14, steps: -2, want: 10},
		{base: 14, steps: -100, want: 10}, // clamped to the floor
		{base: 14, steps: 100, want: 96},  // clamped to the ceiling
		{base: 40, steps: -100, want: 10}, // reapplied onto a smaller disk base still clamps
	}
	for _, c := range cases {
		if got := zoomedFontSize(c.base, c.steps); got != c.want {
			t.Errorf("zoomedFontSize(%d, %d) = %d, want %d", c.base, c.steps, got, c.want)
		}
	}
}

// TestBuildFacesForClampsOversizedScale guards the fix for the app
// crashing at launch — never even opening a window — when cfg.Atlas.Scale
// (now multiplied by DPI, see effectiveAtlasScale) produces an atlas
// texture bigger than the GPU allows: a stale config value should
// degrade to a smaller, working atlas, not take the whole process down.
func TestBuildFacesForClampsOversizedScale(t *testing.T) {
	cfg := config.Config{
		Font:  config.Font{Size: 14, LineHeight: 1},
		Atlas: config.Atlas{Scale: 8, Gamma: 1.0},
	}
	// Small enough that scale=8 (and several steps below it) can't
	// possibly fit, forcing the clamp path, but not so small that even
	// scale=1 fails — this must still succeed, just at a lower scale.
	const smallMaxTextureSize = 4096

	faces, effectiveScale, err := buildFacesFor(cfg, 1, smallMaxTextureSize)
	if err != nil {
		t.Fatalf("buildFacesFor did not clamp down to something that fits: %v", err)
	}
	if effectiveScale >= 8 {
		t.Fatalf("effectiveScale = %d, want it clamped below the requested 8", effectiveScale)
	}
	if faces == nil || faces.Regular == nil {
		t.Fatal("buildFacesFor returned no usable faces")
	}
}

func TestCellFromFramebufferPixelsUsesLetterbox(t *testing.T) {
	cs := &cellSize{w: 10, h: 10, dpiX: 1, dpiY: 1}
	ar := config.AspectRatio{Width: 4, Height: 3}

	x, y := cellFromFramebufferPixels(187, 30, 1000, 500, cs, ar)
	if x != 2 || y != 3 {
		t.Fatalf("cell = (%d,%d), want (2,3)", x, y)
	}

	x, y = cellFromFramebufferPixels(10, 30, 1000, 500, cs, ar)
	if x != 0 || y != 3 {
		t.Fatalf("left bar cell = (%d,%d), want (0,3)", x, y)
	}

	x, y = cellFromFramebufferPixels(990, 30, 1000, 500, cs, ar)
	if x != 66 || y != 3 {
		t.Fatalf("right bar cell = (%d,%d), want (66,3)", x, y)
	}
}

func TestClampCell(t *testing.T) {
	s := screen.New(80, 24)
	x, y := clampCell(s, 99, -4)
	if x != 79 || y != 0 {
		t.Fatalf("clamped cell = (%d,%d), want (79,0)", x, y)
	}
}
