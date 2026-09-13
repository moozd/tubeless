package screen

import (
	"testing"

	"github.com/moozd/tubeless/pkg/vtparse"
)

// parse feeds raw control bytes through a Handler against s.
func parse(s *Screen, in string) {
	h := NewHandler(s)
	p := vtparse.New(h)
	p.Write([]byte(in))
}

func TestKittyQueryResponse(t *testing.T) {
	s := New(80, 24)
	parse(s, "\x1b[?u")
	resps := s.DrainResponses()
	if len(resps) != 1 {
		t.Fatalf("got %d responses, want 1", len(resps))
	}
	if got := string(resps[0]); got != "\x1b[?31u" {
		t.Errorf("query response = %q, want %q", got, "\x1b[?31u")
	}
}

func TestKittySetFlags(t *testing.T) {
	s := New(80, 24)
	parse(s, "\x1b[=9;1u") // flags 9 (1|8), mode 1: replace
	if got := s.KittyFlags(); got != 9 {
		t.Fatalf("flags = %d, want 9", got)
	}
	parse(s, "\x1b[=2;2u") // mode 2: set bit 2, leave others
	if got := s.KittyFlags(); got != 11 {
		t.Fatalf("flags = %d, want 11", got)
	}
	parse(s, "\x1b[=2;3u") // mode 3: clear bit 2
	if got := s.KittyFlags(); got != 9 {
		t.Fatalf("flags = %d, want 9", got)
	}
}

func TestKittyPushPop(t *testing.T) {
	s := New(80, 24)
	parse(s, "\x1b[=1u") // flags = 1
	parse(s, "\x1b[>8u") // push 1, set 8
	if got := s.KittyFlags(); got != 8 {
		t.Fatalf("after push flags = %d, want 8", got)
	}
	parse(s, "\x1b[<u") // pop -> 1
	if got := s.KittyFlags(); got != 1 {
		t.Fatalf("after pop flags = %d, want 1", got)
	}
	parse(s, "\x1b[<u") // pop empty stack -> reset
	if got := s.KittyFlags(); got != 0 {
		t.Fatalf("after empty pop flags = %d, want 0", got)
	}
}

func TestKittyPerScreen(t *testing.T) {
	s := New(80, 24)
	parse(s, "\x1b[=5u")     // main flags = 5
	parse(s, "\x1b[?1049h") // enter alt screen
	if got := s.KittyFlags(); got != 0 {
		t.Fatalf("alt screen flags = %d, want 0 (fresh)", got)
	}
	parse(s, "\x1b[=8u")     // alt flags = 8
	parse(s, "\x1b[?1049l") // exit alt screen
	if got := s.KittyFlags(); got != 5 {
		t.Fatalf("main screen flags = %d, want 5", got)
	}
}

func TestModifyOtherKeys(t *testing.T) {
	s := New(80, 24)
	parse(s, "\x1b[>4;2m")
	if s.ModifyOtherKeys != 2 {
		t.Fatalf("ModifyOtherKeys = %d, want 2", s.ModifyOtherKeys)
	}
	parse(s, "\x1b[>4;0m")
	if s.ModifyOtherKeys != 0 {
		t.Fatalf("ModifyOtherKeys = %d, want 0", s.ModifyOtherKeys)
	}
}

// TestModifyOtherKeysNotSGR guards the pre-existing fix: "CSI > 4;2 m" is
// a private marker and must not be applied as SGR (which would misread its
// params as color/attribute codes and corrupt the current cell).
func TestModifyOtherKeysNotSGR(t *testing.T) {
	s := New(80, 24)
	parse(s, "\x1b[31mX") // red
	s.Cols = 2
	parse(s, "\x1b[>4;2m")
	if s.ModifyOtherKeys != 2 {
		t.Fatalf("ModifyOtherKeys = %d, want 2", s.ModifyOtherKeys)
	}
	// Attribute state must be unchanged by the modifyOtherKeys sequence.
	if s.CurAttr == (Attr{}) {
		t.Fatal("current attribute was unexpectedly reset")
	}
}
