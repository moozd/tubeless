package screen

import "testing"

func TestOSC52SetRecordsPendingClipboard(t *testing.T) {
	p, s := newTestParser(10, 2)
	p.Write([]byte("\x1b]52;c;aGVsbG8=\x07")) // base64 "hello"
	got := s.PendingClipboard()
	if len(got) != 1 || got[0] != "hello" {
		t.Fatalf("PendingClipboard() = %v, want [hello]", got)
	}
}

func TestOSC52QueryIsIgnored(t *testing.T) {
	p, s := newTestParser(10, 2)
	p.Write([]byte("\x1b]52;c;?\x07"))
	if got := s.PendingClipboard(); len(got) != 0 {
		t.Fatalf("PendingClipboard() = %v, want empty (query must not be answered)", got)
	}
}

func TestOSC52MalformedIsIgnored(t *testing.T) {
	p, s := newTestParser(10, 2)
	p.Write([]byte("\x1b]52;c;not-valid-base64!!\x07"))
	if got := s.PendingClipboard(); len(got) != 0 {
		t.Fatalf("PendingClipboard() = %v, want empty (invalid base64)", got)
	}
}

func TestOSC52UnrelatedOSCIsIgnored(t *testing.T) {
	p, s := newTestParser(10, 2)
	p.Write([]byte("\x1b]0;window title\x07")) // OSC 0: set window title, unrelated
	if got := s.PendingClipboard(); len(got) != 0 {
		t.Fatalf("PendingClipboard() = %v, want empty", got)
	}
}

func TestClearPendingClipboardEmptiesQueue(t *testing.T) {
	p, s := newTestParser(10, 2)
	p.Write([]byte("\x1b]52;c;aGVsbG8=\x07"))
	if len(s.PendingClipboard()) == 0 {
		t.Fatal("expected a pending clipboard set before clearing")
	}
	s.ClearPendingClipboard()
	if got := s.PendingClipboard(); len(got) != 0 {
		t.Fatalf("PendingClipboard() len = %d after clear, want 0", len(got))
	}
}
