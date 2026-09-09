package screen

import "testing"

func TestScrollbackCapturesScrolledRows(t *testing.T) {
	p, s := newTestParser(3, 2)
	// Fill two rows, then push a third line — the top row ("abc") scrolls
	// off and should land in scrollback.
	p.Write([]byte("abc\r\ndef\r\nghi"))
	if got := s.ScrollbackLen(); got != 1 {
		t.Fatalf("ScrollbackLen() = %d, want 1", got)
	}
	win := s.VisibleWindow(1)
	if win[0][0].Rune != 'a' || win[0][1].Rune != 'b' || win[0][2].Rune != 'c' {
		t.Fatalf("scrolled-back row = %q%q%q, want abc", win[0][0].Rune, win[0][1].Rune, win[0][2].Rune)
	}
	if win[1][0].Rune != 'd' {
		t.Fatalf("second row of scrolled-back window = %q, want d", win[1][0].Rune)
	}
}

func TestScrollbackCapEviction(t *testing.T) {
	p, s := newTestParser(1, 1)
	s.SetScrollbackCap(2)
	p.Write([]byte("a\r\nb\r\nc\r\nd"))
	if got := s.ScrollbackLen(); got != 2 {
		t.Fatalf("ScrollbackLen() = %d, want 2 (capped)", got)
	}
	win := s.VisibleWindow(2)
	if win[0][0].Rune != 'b' {
		t.Fatalf("oldest retained row = %q, want b (a should've been evicted)", win[0][0].Rune)
	}
}

func TestScrollbackDisabledWhenCapZero(t *testing.T) {
	p, s := newTestParser(1, 1)
	s.SetScrollbackCap(0)
	p.Write([]byte("a\r\nb\r\nc"))
	if got := s.ScrollbackLen(); got != 0 {
		t.Fatalf("ScrollbackLen() = %d, want 0", got)
	}
}

func TestScrollbackSkipsAltScreen(t *testing.T) {
	p, s := newTestParser(3, 2)
	p.Write([]byte("\x1b[?1049h")) // enter alt screen
	p.Write([]byte("abc\r\ndef\r\nghi"))
	if got := s.ScrollbackLen(); got != 0 {
		t.Fatalf("ScrollbackLen() = %d, want 0 (alt-screen scrolling shouldn't capture)", got)
	}
}

func TestScrollbackSkipsPartialScrollRegion(t *testing.T) {
	p, s := newTestParser(3, 4)
	p.Write([]byte("\x1b[2;3r")) // scroll region rows 2-3 only, not the whole screen
	p.Write([]byte("\x1b[S"))    // CSI S: scroll region up 1
	if got := s.ScrollbackLen(); got != 0 {
		t.Fatalf("ScrollbackLen() = %d, want 0 (partial region scroll shouldn't capture)", got)
	}
}

func TestCloneScrollbackIsStableSnapshot(t *testing.T) {
	p, s := newTestParser(1, 1)
	p.Write([]byte("a\r\nb"))
	clone := s.Clone()
	if got := clone.ScrollbackLen(); got != 1 {
		t.Fatalf("clone ScrollbackLen() = %d, want 1", got)
	}
	// Further scrolling on the live Screen must not retroactively change
	// what the earlier clone sees (COW immutability contract).
	p.Write([]byte("\r\nc\r\nd"))
	if got := clone.ScrollbackLen(); got != 1 {
		t.Fatalf("clone ScrollbackLen() changed to %d after further scrolling on the source, want still 1", got)
	}
	if got := s.ScrollbackLen(); got != 3 {
		t.Fatalf("live ScrollbackLen() = %d, want 3", got)
	}
}
