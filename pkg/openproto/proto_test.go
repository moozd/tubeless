package openproto

import (
	"bytes"
	"io"
	"testing"
)

func roundtrip(t *testing.T, msg any) any {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatalf("WriteMessage(%T): %v", msg, err)
	}
	got, err := ReadMessage(NewReader(&buf))
	if err != nil {
		t.Fatalf("ReadMessage after %T: %v", msg, err)
	}
	return got
}

func TestRoundtripOpenRequest(t *testing.T) {
	want := OpenRequest{Title: "neovim"}
	got := roundtrip(t, want)
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestRoundtripOpenAck(t *testing.T) {
	got := roundtrip(t, OpenAck{})
	if _, ok := got.(OpenAck); !ok {
		t.Fatalf("got %T, want OpenAck", got)
	}
}

func TestRoundtripOpenReject(t *testing.T) {
	want := OpenReject{Reason: "session already active"}
	got := roundtrip(t, want)
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestRoundtripResize(t *testing.T) {
	want := Resize{W: 1920, H: 1080}
	got := roundtrip(t, want)
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestRoundtripClose(t *testing.T) {
	want := Close{Reason: "app exited"}
	got := roundtrip(t, want)
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestRoundtripInputEvent(t *testing.T) {
	cases := []InputEvent{
		{Kind: InputPointerMove, X: 10, Y: 20},
		{Kind: InputPointerButton, X: 5, Y: 6, Button: PointerLeft, Pressed: true},
		{Kind: InputPointerScroll, X: 1, Y: 2, Scroll: -3},
		{Kind: InputKey, Code: KeyControlL, Pressed: true},
		{Kind: InputKey, Rune: 'a', Pressed: true},
	}
	for _, want := range cases {
		got := roundtrip(t, want)
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	}
}

func TestRoundtripFrameRect(t *testing.T) {
	pix := make([]byte, 4*4*4)
	for i := range pix {
		pix[i] = byte(i)
	}
	want := FrameRect{X: 1, Y: 2, W: 4, H: 4, Pix: pix}
	got := roundtrip(t, want).(FrameRect)
	if got.X != want.X || got.Y != want.Y || got.W != want.W || got.H != want.H {
		t.Fatalf("got geometry %+v, want %+v", got, want)
	}
	if !bytes.Equal(got.Pix, want.Pix) {
		t.Fatalf("pixel mismatch")
	}
}

func TestFrameRectBadPixelLength(t *testing.T) {
	// W*H*4 must match len(Pix) exactly — a hand-built bad frame (as if
	// corrupted on the wire) must be rejected, not silently truncated or
	// out-of-bounds read.
	var buf bytes.Buffer
	if err := WriteMessage(&buf, FrameRect{X: 0, Y: 0, W: 2, H: 2, Pix: make([]byte, 16)}); err != nil {
		t.Fatalf("write: %v", err)
	}
	raw := buf.Bytes()
	// Corrupt the payload length prefix (header bytes 6:10, big-endian
	// uint32) to claim one byte fewer than the real payload actually is.
	raw[9]--
	if _, err := ReadMessage(NewReader(bytes.NewReader(raw[:len(raw)-1]))); err == nil {
		t.Fatalf("expected an error decoding a truncated FrameRect, got nil")
	}
}

func TestReadMessageBadMagic(t *testing.T) {
	_, err := ReadMessage(NewReader(bytes.NewReader([]byte("XXXXXXXXXX"))))
	if err == nil {
		t.Fatalf("expected bad-magic error, got nil")
	}
}

func TestReadMessageEOF(t *testing.T) {
	_, err := ReadMessage(NewReader(bytes.NewReader(nil)))
	if err != io.EOF {
		t.Fatalf("got %v, want io.EOF", err)
	}
}

func TestMsgTypeString(t *testing.T) {
	if MsgFrameRect.String() != "FrameRect" {
		t.Fatalf("got %q", MsgFrameRect.String())
	}
	if MsgType(99).String() == "" {
		t.Fatalf("unknown type should still stringify to something non-empty")
	}
}
