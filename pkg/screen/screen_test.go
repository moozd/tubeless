package screen

import (
	"testing"

	"github.com/moozd/tubeless/pkg/vtparse"
)

func newTestParser(cols, rows int) (*vtparse.Parser, *Screen) {
	s := New(cols, rows)
	h := NewHandler(s)
	return vtparse.New(h), s
}

func TestPrintAdvancesCursor(t *testing.T) {
	p, s := newTestParser(10, 5)
	p.Write([]byte("hi"))
	if s.CursorX != 2 {
		t.Fatalf("CursorX = %d, want 2", s.CursorX)
	}
	if s.Grid[0][0].Rune != 'h' || s.Grid[0][1].Rune != 'i' {
		t.Fatalf("unexpected grid content: %q %q", s.Grid[0][0].Rune, s.Grid[0][1].Rune)
	}
}

func TestAutowrapAndScroll(t *testing.T) {
	p, s := newTestParser(3, 2)
	p.Write([]byte("abcdef"))
	if s.Grid[0][0].Rune != 'a' || s.Grid[1][0].Rune != 'd' {
		t.Fatalf("unexpected wrap: row0=%q row1=%q", string(s.Grid[0][0].Rune), string(s.Grid[1][0].Rune))
	}
	p.Write([]byte("ghi"))
	if s.Grid[0][0].Rune != 'd' || s.Grid[1][0].Rune != 'g' {
		t.Fatalf("unexpected scroll: row0=%q row1=%q", string(s.Grid[0][0].Rune), string(s.Grid[1][0].Rune))
	}
}

func TestCUPMovesCursor(t *testing.T) {
	p, s := newTestParser(80, 24)
	p.Write([]byte("\x1b[10;5H"))
	if s.CursorX != 4 || s.CursorY != 9 {
		t.Fatalf("cursor = (%d,%d), want (4,9)", s.CursorX, s.CursorY)
	}
}

func TestSGRBold(t *testing.T) {
	p, s := newTestParser(80, 24)
	p.Write([]byte("\x1b[1mX\x1b[0mY"))
	if !s.Grid[0][0].Attr.Bold {
		t.Fatal("expected bold on first cell")
	}
	if s.Grid[0][1].Attr.Bold {
		t.Fatal("expected bold cleared on second cell")
	}
}

func TestEraseInLine(t *testing.T) {
	p, s := newTestParser(5, 1)
	p.Write([]byte("abcde"))
	p.Write([]byte("\x1b[3D\x1b[K"))
	if s.Grid[0][0].Rune != 'a' || s.Grid[0][2].Rune != ' ' {
		t.Fatalf("EL failed: %q %q", s.Grid[0][0].Rune, s.Grid[0][2].Rune)
	}
}

func TestDECSpecialGraphics(t *testing.T) {
	p, s := newTestParser(80, 24)
	p.Write([]byte("\x1b(0lqk\x1b(B"))
	if s.Grid[0][0].Rune != '┌' || s.Grid[0][1].Rune != '─' || s.Grid[0][2].Rune != '┐' {
		t.Fatalf("unexpected DEC graphics translation: %q", []rune{s.Grid[0][0].Rune, s.Grid[0][1].Rune, s.Grid[0][2].Rune})
	}
}

func TestScrollRegion(t *testing.T) {
	p, s := newTestParser(3, 4)
	p.Write([]byte("\x1b[2;3r"))
	if s.ScrollTop != 1 || s.ScrollBottom != 2 {
		t.Fatalf("scroll region = (%d,%d), want (1,2)", s.ScrollTop, s.ScrollBottom)
	}
}

func TestUTF8Print(t *testing.T) {
	p, s := newTestParser(10, 1)
	p.Write([]byte("café"))
	got := string([]rune{s.Grid[0][0].Rune, s.Grid[0][1].Rune, s.Grid[0][2].Rune, s.Grid[0][3].Rune})
	if got != "café" {
		t.Fatalf("got %q, want café", got)
	}
}
