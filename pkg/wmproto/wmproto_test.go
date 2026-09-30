//go:build linux

package wmproto

import (
	"os"
	"reflect"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	msgs := []any{
		Resize{640, 400},
		PointerMove{-3, 512},
		PointerButton{3, true},
		PointerAxis{0, -2},
		Key{0x01000041, true},
		Focus{true},
		Hello{Version},
		Frame{1, 2, 3, 4},
		Exited{-1},
	}
	for _, m := range msgs {
		b, err := encode(m)
		if err != nil {
			t.Fatalf("encode %T: %v", m, err)
		}
		got, err := decode(b)
		if err != nil {
			t.Fatalf("decode %T: %v", m, err)
		}
		if !reflect.DeepEqual(got, m) {
			t.Errorf("round trip %T: got %+v, want %+v", m, got, m)
		}
	}
}

func TestDecodeRejectsTruncatedAndUnknown(t *testing.T) {
	for _, b := range [][]byte{{}, {typeResize, 1, 2}, {0x7f}} {
		if _, err := decode(b); err == nil {
			t.Errorf("decode(%v) succeeded, want error", b)
		}
	}
}

func TestBufferCarriesFileDescriptor(t *testing.T) {
	win, theirs, err := NewSocketpair()
	if err != nil {
		t.Fatal(err)
	}
	defer win.Close()
	other, err := NewConn(theirs)
	theirs.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()

	f, err := os.CreateTemp(t.TempDir(), "fd")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("pixels"); err != nil {
		t.Fatal(err)
	}
	if err := other.Send(Buffer{W: 2, H: 3, Stride: 8, FD: int(f.Fd())}); err != nil {
		t.Fatal(err)
	}
	msg, err := win.Recv()
	if err != nil {
		t.Fatal(err)
	}
	got, ok := msg.(Buffer)
	if !ok || got.W != 2 || got.H != 3 || got.Stride != 8 || got.FD < 0 {
		t.Fatalf("got %+v, want Buffer{2,3,8,fd}", msg)
	}
	recv := os.NewFile(uintptr(got.FD), "recv")
	defer recv.Close()
	data := make([]byte, 6)
	if _, err := recv.ReadAt(data, 0); err != nil || string(data) != "pixels" {
		t.Fatalf("read through received fd: %q, %v", data, err)
	}
}
