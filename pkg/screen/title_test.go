package screen

import "testing"

func TestOSC2SetsTitle(t *testing.T) {
	p, s := newTestParser(10, 2)
	p.Write([]byte("\x1b]2;my session\x07"))
	if s.Title != "my session" {
		t.Fatalf("Title = %q, want %q", s.Title, "my session")
	}
}

func TestOSC0SetsTitle(t *testing.T) {
	p, s := newTestParser(10, 2)
	p.Write([]byte("\x1b]0;icon and title\x07"))
	if s.Title != "icon and title" {
		t.Fatalf("Title = %q, want %q", s.Title, "icon and title")
	}
}

func TestOSC1IconNameOnlyLeavesTitleUnset(t *testing.T) {
	p, s := newTestParser(10, 2)
	p.Write([]byte("\x1b]1;icon name\x07"))
	if s.Title != "" {
		t.Fatalf("Title = %q, want empty (OSC 1 is icon name only)", s.Title)
	}
}

func TestOSCTitleLaterSetOverridesEarlier(t *testing.T) {
	p, s := newTestParser(10, 2)
	p.Write([]byte("\x1b]2;first\x07\x1b]2;second\x07"))
	if s.Title != "second" {
		t.Fatalf("Title = %q, want %q", s.Title, "second")
	}
}
