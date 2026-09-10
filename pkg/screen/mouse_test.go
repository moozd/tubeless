package screen

import "testing"

func TestEncodeMouseEvent(t *testing.T) {
	got := string(EncodeMouseEvent(true, MouseButtonLeft, MousePress, 4, 9, false, false, false))
	if want := "\x1b[<0;5;10M"; got != want {
		t.Fatalf("EncodeMouseEvent press = %q, want %q", got, want)
	}
	got = string(EncodeMouseEvent(true, MouseButtonLeft, MouseRelease, 4, 9, false, false, false))
	if want := "\x1b[<0;5;10m"; got != want {
		t.Fatalf("EncodeMouseEvent release = %q, want %q", got, want)
	}
	got = string(EncodeMouseEvent(true, MouseWheelUp, MousePress, 0, 0, true, false, false))
	if want := "\x1b[<68;1;1M"; got != want {
		t.Fatalf("EncodeMouseEvent wheel+shift = %q, want %q", got, want)
	}
}

func TestEncodeMouseEventLegacy(t *testing.T) {
	got := EncodeMouseEvent(false, MouseButtonLeft, MousePress, 4, 9, false, false, false)
	want := []byte{0x1b, '[', 'M', 32, 32 + 5, 32 + 10}
	if string(got) != string(want) {
		t.Fatalf("EncodeMouseEvent legacy press = %v, want %v", got, want)
	}
	// Release reports code 3 regardless of which button, per the legacy spec.
	got = EncodeMouseEvent(false, MouseButtonRight, MouseRelease, 4, 9, false, false, false)
	want = []byte{0x1b, '[', 'M', 32 + 3, 32 + 5, 32 + 10}
	if string(got) != string(want) {
		t.Fatalf("EncodeMouseEvent legacy release = %v, want %v", got, want)
	}
	// Coordinates clamp at the format's 223 ceiling instead of wrapping.
	got = EncodeMouseEvent(false, MouseButtonLeft, MousePress, 500, 500, false, false, false)
	want = []byte{0x1b, '[', 'M', 32, 32 + 223, 32 + 223}
	if string(got) != string(want) {
		t.Fatalf("EncodeMouseEvent legacy clamp = %v, want %v", got, want)
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
