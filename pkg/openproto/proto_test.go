package openproto

import (
	"bytes"
	"reflect"
	"testing"
)

func TestMessagesRoundTrip(t *testing.T) {
	msgs := []any{
		Open{Argv: []string{"foot", "-e", "htop"}, Env: []string{"A=1", "B="}, Cwd: "/tmp", Cols: 120, Rows: 40},
		Open{Argv: []string{"x"}, Env: []string{}},
		OpenAck{SlotID: 7},
		OpenReject{Reason: "no"},
		Key{Keysym: 0x01000041, Pressed: true},
		Focus{Focused: true},
		Resize{Cols: 80, Rows: 24},
		Exited{Code: -1},
		Close{Reason: "bye"},
	}
	for _, m := range msgs {
		var buf bytes.Buffer
		if err := WriteMessage(&buf, m); err != nil {
			t.Fatalf("write %T: %v", m, err)
		}
		got, err := ReadMessage(NewReader(&buf))
		if err != nil {
			t.Fatalf("read %T: %v", m, err)
		}
		if !reflect.DeepEqual(got, m) {
			t.Errorf("round trip: got %+v, want %+v", got, m)
		}
	}
}

func TestReadRejectsBadFrames(t *testing.T) {
	cases := map[string][]byte{
		"bad magic":    append([]byte("NOPE"), make([]byte, 6)...),
		"bad version":  {'T', 'B', 'O', 'P', 1, byte(MsgFocus), 0, 0, 0, 1, 0},
		"short string": {'T', 'B', 'O', 'P', version, byte(MsgClose), 0, 0, 0, 4, 0, 0, 0, 9},
		"unknown type": {'T', 'B', 'O', 'P', version, 99, 0, 0, 0, 0},
	}
	for name, b := range cases {
		if _, err := ReadMessage(bytes.NewReader(b)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}
