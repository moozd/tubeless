package screen

import (
	"fmt"
	"testing"
)

func TestAppletCellRoundTripsThroughSGR(t *testing.T) {
	for _, id := range []int{0, 1, 239, 240, 12345, AppletMaxID - 1} {
		p, s := newTestParser(4, 2)
		fg, bg := AppletColors(id)
		p.Write([]byte(fmt.Sprintf("\x1b[38;5;%dm\x1b[48;5;%dm%c", fg, bg, AppletRune(200, 254))))
		gotID, col, row, ok := s.Grid[0][0].AppletCell()
		if !ok || gotID != id || col != 200 || row != 254 {
			t.Fatalf("id %d: AppletCell() = %d, %d, %d, %v", id, gotID, col, row, ok)
		}
	}
}

func TestAppletCellRejectsPlainCells(t *testing.T) {
	p, s := newTestParser(4, 1)
	p.Write([]byte("\x1b[38;5;20mx"))
	if _, _, _, ok := s.Grid[0][0].AppletCell(); ok {
		t.Fatal("plain cell read as an applet cell")
	}
	p.Write([]byte(fmt.Sprintf("\x1b[0m%c", AppletRune(0, 0))))
	if _, _, _, ok := s.Grid[0][1].AppletCell(); ok {
		t.Fatal("placeholder rune with no colors read as an applet cell")
	}
}

func TestOSCAppletQueuesDecodedPayload(t *testing.T) {
	p, s := newTestParser(4, 1)
	p.Write([]byte("\x1b]7777;aGVsbG8=\x1b\\"))
	got := s.DrainAppletRequests()
	if len(got) != 1 || string(got[0]) != "hello" {
		t.Fatalf("DrainAppletRequests() = %q, want [hello]", got)
	}
	if again := s.DrainAppletRequests(); len(again) != 0 {
		t.Fatalf("second drain = %q, want empty", again)
	}
}
