package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/moozd/tubeless/pkg/render"
	"github.com/moozd/tubeless/pkg/screen"
	"github.com/moozd/tubeless/pkg/vtparse"
)

// placeApplet writes applet id's cells for applet columns c0..c1-1 of
// applet row ar at screen row y, starting at screen column x.
func placeApplet(scr *screen.Screen, id, x, y, ar, c0, c1 int) {
	fg, bg := screen.AppletColors(id)
	var b strings.Builder
	fmt.Fprintf(&b, "\x1b[%d;%dH\x1b[38;5;%dm\x1b[48;5;%dm", y+1, x+1, fg, bg)
	for c := c0; c < c1; c++ {
		b.WriteRune(screen.AppletRune(c, ar))
	}
	vtparse.New(screen.NewHandler(scr)).Write([]byte(b.String()))
}

func TestAppletSpansMergeRunsAndKeepTexturePosition(t *testing.T) {
	scr := screen.New(10, 3)
	placeApplet(scr, 7, 2, 1, 0, 0, 4)
	// A popup covered applet columns 2-3 of row 1: only 4-5 show.
	placeApplet(scr, 7, 6, 2, 1, 4, 6)
	got := appletSpans(scr, map[int]appletGrid{7: {cols: 8, rows: 2}})
	want := []render.AppletSpan{
		{ID: 7, Col: 2, Row: 1, Cells: 4, U0: 0, V0: 0, U1: 0.5, V1: 0.5},
		{ID: 7, Col: 6, Row: 2, Cells: 2, U0: 0.5, V0: 0.5, U1: 0.75, V1: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("spans = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("span %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestAppletSpansSkipClosedApplets(t *testing.T) {
	scr := screen.New(4, 1)
	placeApplet(scr, 3, 0, 0, 0, 0, 4)
	if got := appletSpans(scr, map[int]appletGrid{9: {cols: 4, rows: 1}}); len(got) != 0 {
		t.Fatalf("spans for an applet that is not open: %+v", got)
	}
}

func TestIsExecSocketAcceptsOnlyLocalExecSockets(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	cases := map[string]bool{
		"/run/user/1000/tubeless/exec-123.sock":      true,
		"/run/user/1000/wayland-1":                   false,
		"/run/user/1000/bus":                         false,
		"/var/run/docker.sock":                       false,
		"/run/user/1000/tubeless/exec-.sock":         false,
		"/run/user/1000/tubeless/exec-12x.sock":      false,
		"/run/user/1000/tubeless/../bus/exec-1.sock": false,
		"/run/user/1000/tubeless/sub/exec-1.sock":    false,
	}
	for path, want := range cases {
		if got := isExecSocket(path); got != want {
			t.Errorf("isExecSocket(%q) = %v, want %v", path, got, want)
		}
	}
}
