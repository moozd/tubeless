package screen

import "testing"

func TestEncodeMouseEvent(t *testing.T) {
	got := string(EncodeMouseEvent(MouseButtonLeft, MousePress, 4, 9, false, false, false))
	if want := "\x1b[<0;5;10M"; got != want {
		t.Fatalf("EncodeMouseEvent press = %q, want %q", got, want)
	}
	got = string(EncodeMouseEvent(MouseButtonLeft, MouseRelease, 4, 9, false, false, false))
	if want := "\x1b[<0;5;10m"; got != want {
		t.Fatalf("EncodeMouseEvent release = %q, want %q", got, want)
	}
	got = string(EncodeMouseEvent(MouseWheelUp, MousePress, 0, 0, true, false, false))
	if want := "\x1b[<68;1;1M"; got != want {
		t.Fatalf("EncodeMouseEvent wheel+shift = %q, want %q", got, want)
	}
}

func TestMouseModeCSI(t *testing.T) {
	p, s := newTestParser(80, 24)
	p.Write([]byte("\x1b[?1002h\x1b[?1006h\x1b[?2004h"))
	if s.MouseMode != MouseDrag {
		t.Fatalf("MouseMode = %v, want MouseDrag", s.MouseMode)
	}
	if !s.MouseSGR {
		t.Fatal("MouseSGR = false, want true")
	}
	if !s.BracketedPaste {
		t.Fatal("BracketedPaste = false, want true")
	}
	p.Write([]byte("\x1b[?1002l"))
	if s.MouseMode != MouseOff {
		t.Fatalf("MouseMode = %v, want MouseOff after ?1002l", s.MouseMode)
	}
}
