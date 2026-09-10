package main

import (
	"sync/atomic"
	"testing"
	"time"

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
