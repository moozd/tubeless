package screen

import "testing"

func TestLineFeedAtMarginRecordsScrollUp(t *testing.T) {
	p, s := newTestParser(3, 2)
	p.Write([]byte("abc\r\ndef\r\nghi")) // third line pushes row 0 off the top
	got := s.PendingScrolls()
	if len(got) != 1 {
		t.Fatalf("PendingScrolls() len = %d, want 1", len(got))
	}
	want := ScrollShift{Top: 0, Bottom: 1, Delta: 1}
	if got[0] != want {
		t.Fatalf("shift = %+v, want %+v", got[0], want)
	}
}

func TestReverseIndexAtTopMarginRecordsScrollDown(t *testing.T) {
	p, s := newTestParser(3, 2)
	p.Write([]byte("abc\x1bM")) // RI at row 0 (== ScrollTop) scrolls down
	got := s.PendingScrolls()
	if len(got) != 1 {
		t.Fatalf("PendingScrolls() len = %d, want 1", len(got))
	}
	want := ScrollShift{Top: 0, Bottom: 1, Delta: -1}
	if got[0] != want {
		t.Fatalf("shift = %+v, want %+v", got[0], want)
	}
}

func TestExplicitScrollUpDownRecordShifts(t *testing.T) {
	p, s := newTestParser(5, 5)
	p.Write([]byte("\x1b[2S")) // SU 2: scroll up by 2
	p.Write([]byte("\x1b[1T")) // SD 1: scroll down by 1
	got := s.PendingScrolls()
	if len(got) != 2 {
		t.Fatalf("PendingScrolls() len = %d, want 2", len(got))
	}
	if got[0] != (ScrollShift{Top: 0, Bottom: 4, Delta: 2}) {
		t.Fatalf("first shift = %+v, want {0 4 2}", got[0])
	}
	if got[1] != (ScrollShift{Top: 0, Bottom: 4, Delta: -1}) {
		t.Fatalf("second shift = %+v, want {0 4 -1}", got[1])
	}
}

func TestInsertLinesRecordsNegativeShift(t *testing.T) {
	p, s := newTestParser(5, 5)
	p.Write([]byte("\x1b[2;1H"))  // cursor to row 1 (0-based)
	p.Write([]byte("\x1b[2L"))    // IL 2: insert 2 blank lines, shifting rest down
	got := s.PendingScrolls()
	if len(got) != 1 {
		t.Fatalf("PendingScrolls() len = %d, want 1", len(got))
	}
	want := ScrollShift{Top: 1, Bottom: 4, Delta: -2}
	if got[0] != want {
		t.Fatalf("shift = %+v, want %+v", got[0], want)
	}
}

func TestDeleteLinesRecordsPositiveShift(t *testing.T) {
	p, s := newTestParser(5, 5)
	p.Write([]byte("\x1b[2;1H")) // cursor to row 1 (0-based)
	p.Write([]byte("\x1b[2M"))   // DL 2: delete 2 lines, shifting rest up
	got := s.PendingScrolls()
	if len(got) != 1 {
		t.Fatalf("PendingScrolls() len = %d, want 1", len(got))
	}
	want := ScrollShift{Top: 1, Bottom: 4, Delta: 2}
	if got[0] != want {
		t.Fatalf("shift = %+v, want %+v", got[0], want)
	}
}

func TestClearPendingScrollsEmptiesQueue(t *testing.T) {
	p, s := newTestParser(3, 2)
	p.Write([]byte("abc\r\ndef\r\nghi"))
	if len(s.PendingScrolls()) == 0 {
		t.Fatal("expected a pending scroll before clearing")
	}
	s.ClearPendingScrolls()
	if got := s.PendingScrolls(); len(got) != 0 {
		t.Fatalf("PendingScrolls() len = %d after clear, want 0", len(got))
	}
}

func TestCloneCarriesPendingScrollsIndependently(t *testing.T) {
	p, s := newTestParser(3, 2)
	p.Write([]byte("abc\r\ndef\r\nghi"))
	c := s.Clone()
	s.ClearPendingScrolls()
	if len(s.PendingScrolls()) != 0 {
		t.Fatal("source should be cleared")
	}
	if len(c.PendingScrolls()) != 1 {
		t.Fatalf("clone's PendingScrolls() len = %d, want 1 (unaffected by clearing the source)", len(c.PendingScrolls()))
	}
}
