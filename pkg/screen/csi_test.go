package screen

import (
	"testing"

	"github.com/moozd/tubeless/pkg/vtparse"
)

// TestPrivateMarkedMIsNotSGR reproduces a real Claude Code startup
// sequence, xterm's "CSI > 4;2 m" modifyOtherKeys negotiation. Its final
// byte is 'm' but the '>' private marker means it isn't SGR at all — a
// naive dispatch on final byte alone misreads params [4, 2] as SGR
// underline+dim, which then never gets reset because the app never
// touched SGR in the first place.
func TestPrivateMarkedMIsNotSGR(t *testing.T) {
	s := New(80, 24)
	p := vtparse.New(NewHandler(s))
	p.Write([]byte("\x1b[>4;2mX"))
	if s.Grid[0][0].Attr.Underline != UnderlineNone {
		t.Fatalf("Underline = %v, want none (CSI > 4;2 m is not SGR)", s.Grid[0][0].Attr.Underline)
	}
	if s.Grid[0][0].Attr.Dim {
		t.Fatal("Dim = true, want false (CSI > 4;2 m is not SGR)")
	}
}
