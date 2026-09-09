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

// TestScrollUpDownMultiLineOrder locks in the single-copy ScrollUp/
// ScrollDown rewrite's correctness for n > 1 in one call, since the
// previous per-line loop and the current single shift-and-fill must
// produce identical row contents and scrollback order.
func TestScrollUpDownMultiLineOrder(t *testing.T) {
	p, s := newTestParser(1, 5)
	p.Write([]byte("1\r\n2\r\n3\r\n4\r\n5"))

	p.Write([]byte("\x1b[3S")) // CSI 3S: scroll the whole screen up 3
	want := []rune{'4', '5', ' ', ' ', ' '}
	for y, w := range want {
		if got := s.Grid[y][0].Rune; got != w {
			t.Fatalf("after scroll up 3, row %d = %q, want %q", y, got, w)
		}
	}
	if got := s.ScrollbackLen(); got != 3 {
		t.Fatalf("ScrollbackLen() = %d, want 3", got)
	}
	win := s.VisibleWindow(3)
	for i, w := range []rune{'1', '2', '3'} {
		if got := win[i][0].Rune; got != w {
			t.Fatalf("scrollback row %d = %q, want %q", i, got, w)
		}
	}

	p.Write([]byte("\x1b[2T")) // CSI 2T: scroll the whole screen down 2
	want = []rune{' ', ' ', '4', '5', ' '}
	for y, w := range want {
		if got := s.Grid[y][0].Rune; got != w {
			t.Fatalf("after scroll down 2, row %d = %q, want %q", y, got, w)
		}
	}
}

// TestInsertDeleteLinesMultiLineOrder is InsertLines/DeleteLines' equivalent
// of TestScrollUpDownMultiLineOrder above.
func TestInsertDeleteLinesMultiLineOrder(t *testing.T) {
	p, s := newTestParser(1, 5)
	p.Write([]byte("1\r\n2\r\n3\r\n4\r\n5"))
	p.Write([]byte("\x1b[1;1H")) // cursor to row 0, so the whole screen is "below" it

	p.Write([]byte("\x1b[2L")) // CSI 2L: insert 2 blank lines at the cursor row
	want := []rune{' ', ' ', '1', '2', '3'}
	for y, w := range want {
		if got := s.Grid[y][0].Rune; got != w {
			t.Fatalf("after insert 2 lines, row %d = %q, want %q", y, got, w)
		}
	}

	p.Write([]byte("\x1b[2M")) // CSI 2M: delete 2 lines at the cursor row
	want = []rune{'1', '2', '3', ' ', ' '}
	for y, w := range want {
		if got := s.Grid[y][0].Rune; got != w {
			t.Fatalf("after delete 2 lines, row %d = %q, want %q", y, got, w)
		}
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
