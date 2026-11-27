package applet

import (
	"bytes"
	"encoding/base64"
	"reflect"
	"strings"
	"testing"
)

func TestEncodeRequestRoundTrips(t *testing.T) {
	want := Request{ID: 42, Socket: "/run/x.sock", Name: "pdf", Args: []string{"a b.pdf"}, Cwd: "/tmp", Cols: 80, Rows: 24}
	osc, err := EncodeRequest(want, false)
	if err != nil {
		t.Fatal(err)
	}
	payload := strings.TrimSuffix(strings.TrimPrefix(osc, "\x1b]7777;"), "\x1b\\")
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("payload is not base64: %v", err)
	}
	got, err := DecodeRequest(data)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("DecodeRequest = %+v, %v; want %+v", got, err, want)
	}
}

func TestEncodeRequestWrapsForTmux(t *testing.T) {
	osc, err := EncodeRequest(Request{Name: "x"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(osc, "\x1bPtmux;\x1b\x1b]7777;") || !strings.HasSuffix(osc, "\x1b\x1b\\\x1b\\") {
		t.Fatalf("tmux passthrough = %q", osc)
	}
}

func TestMessagesRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	sent := []Message{{Type: MsgReady}, {Type: MsgExit, Code: 3, Output: "line\n"}, {Type: MsgFocus, Focused: true}}
	for _, m := range sent {
		if err := WriteMessage(&buf, m); err != nil {
			t.Fatal(err)
		}
	}
	out := make(chan Message, len(sent))
	if err := ReadMessages(&buf, out); err != nil {
		t.Fatal(err)
	}
	var got []Message
	for m := range out {
		got = append(got, m)
	}
	if !reflect.DeepEqual(got, sent) {
		t.Fatalf("got %+v, want %+v", got, sent)
	}
}
