// Package openproto is the private wire protocol between a tubeless
// window and a `tubeless open` process running in one of its tmux panes.
// The pane process asks the window to run a GUI app in a slot, forwards
// the keys and focus changes tmux delivers to that pane, and learns when
// the app exits. Pixels never travel here: the window receives them
// straight from the app's compositor (see pkg/wmproto).
//
// Both ends are the same binary over a local Unix socket, so the format
// is a fixed header plus a manual per-message encoding, no dependency.
package openproto

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
)

var magic = [4]byte{'T', 'B', 'O', 'P'}

// version is bumped on any wire-incompatible change. Reader refuses a
// mismatched frame instead of misdecoding it.
const version = 2

const maxPayload = 1 << 20

type MsgType byte

const (
	MsgOpen MsgType = iota + 1
	MsgOpenAck
	MsgOpenReject
	MsgKey
	MsgFocus
	MsgResize
	MsgExited
	MsgClose
)

func (t MsgType) String() string {
	names := map[MsgType]string{
		MsgOpen: "Open", MsgOpenAck: "OpenAck", MsgOpenReject: "OpenReject",
		MsgKey: "Key", MsgFocus: "Focus", MsgResize: "Resize",
		MsgExited: "Exited", MsgClose: "Close",
	}
	if n, ok := names[t]; ok {
		return n
	}
	return fmt.Sprintf("MsgType(%d)", byte(t))
}

// Open asks the window to run Argv in a new slot. Env and Cwd are the
// pane's own, so the app sees the environment the user launched it from.
// Cols/Rows are the pane's size in cells.
type Open struct {
	Argv       []string
	Env        []string
	Cwd        string
	Cols, Rows int
}

// OpenAck reports the slot the app was given; the pane process paints
// its cells with screen.SlotColorIndex(SlotID).
type OpenAck struct{ SlotID int }

type OpenReject struct{ Reason string }

// Key is one key event in X11 keysym terms (Unicode characters use the
// 0x01000000 + code point range).
type Key struct {
	Keysym  uint32
	Pressed bool
}

type Focus struct{ Focused bool }

// Resize reports the pane's new size in cells.
type Resize struct{ Cols, Rows int }

// Exited is sent window->pane when the app has exited.
type Exited struct{ Code int }

// Close is a clean-teardown request in either direction.
type Close struct{ Reason string }

func WriteMessage(w io.Writer, msg any) error {
	t, payload, err := encode(msg)
	if err != nil {
		return err
	}
	if len(payload) > maxPayload {
		return fmt.Errorf("openproto: %s payload %d bytes exceeds max %d", t, len(payload), maxPayload)
	}
	frame := make([]byte, 10, 10+len(payload))
	copy(frame, magic[:])
	frame[4] = version
	frame[5] = byte(t)
	binary.BigEndian.PutUint32(frame[6:], uint32(len(payload)))
	frame = append(frame, payload...)
	if _, err := w.Write(frame); err != nil {
		return fmt.Errorf("openproto: write %s: %w", t, err)
	}
	return nil
}

func ReadMessage(r io.Reader) (any, error) {
	var header [10]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	if [4]byte(header[:4]) != magic {
		return nil, fmt.Errorf("openproto: bad magic %x", header[:4])
	}
	if header[4] != version {
		return nil, fmt.Errorf("openproto: unsupported version %d (want %d)", header[4], version)
	}
	t := MsgType(header[5])
	n := binary.BigEndian.Uint32(header[6:])
	if n > maxPayload {
		return nil, fmt.Errorf("openproto: %s payload %d bytes exceeds max %d", t, n, maxPayload)
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("openproto: read %s payload: %w", t, err)
	}
	return decode(t, payload)
}

func NewReader(r io.Reader) *bufio.Reader {
	return bufio.NewReaderSize(r, 32*1024)
}

func encode(msg any) (MsgType, []byte, error) {
	switch m := msg.(type) {
	case Open:
		b := appendStrings(nil, m.Argv)
		b = appendStrings(b, m.Env)
		b = appendString(b, m.Cwd)
		b = appendInt(b, m.Cols)
		return MsgOpen, appendInt(b, m.Rows), nil
	case OpenAck:
		return MsgOpenAck, appendInt(nil, m.SlotID), nil
	case OpenReject:
		return MsgOpenReject, appendString(nil, m.Reason), nil
	case Key:
		b := binary.BigEndian.AppendUint32(nil, m.Keysym)
		return MsgKey, append(b, boolByte(m.Pressed)), nil
	case Focus:
		return MsgFocus, []byte{boolByte(m.Focused)}, nil
	case Resize:
		return MsgResize, appendInt(appendInt(nil, m.Cols), m.Rows), nil
	case Exited:
		return MsgExited, appendInt(nil, m.Code), nil
	case Close:
		return MsgClose, appendString(nil, m.Reason), nil
	}
	return 0, nil, fmt.Errorf("openproto: cannot encode %T", msg)
}

func decode(t MsgType, b []byte) (any, error) {
	d := &decoder{b: b}
	var msg any
	switch t {
	case MsgOpen:
		o := Open{Argv: d.strings(), Env: d.strings(), Cwd: d.string()}
		o.Cols, o.Rows = d.int(), d.int()
		msg = o
	case MsgOpenAck:
		msg = OpenAck{d.int()}
	case MsgOpenReject:
		msg = OpenReject{d.string()}
	case MsgKey:
		k := Key{Keysym: uint32(d.u32())}
		k.Pressed = d.byte() != 0
		msg = k
	case MsgFocus:
		msg = Focus{d.byte() != 0}
	case MsgResize:
		r := Resize{Cols: d.int()}
		r.Rows = d.int()
		msg = r
	case MsgExited:
		msg = Exited{d.int()}
	case MsgClose:
		msg = Close{d.string()}
	default:
		return nil, fmt.Errorf("openproto: unknown message type %s", t)
	}
	if d.err != nil {
		return nil, fmt.Errorf("openproto: decode %s: %w", t, d.err)
	}
	return msg, nil
}

func boolByte(v bool) byte {
	if v {
		return 1
	}
	return 0
}

func appendInt(b []byte, v int) []byte {
	return binary.BigEndian.AppendUint32(b, uint32(int32(v)))
}

func appendString(b []byte, s string) []byte {
	b = binary.BigEndian.AppendUint32(b, uint32(len(s)))
	return append(b, s...)
}

func appendStrings(b []byte, ss []string) []byte {
	b = binary.BigEndian.AppendUint32(b, uint32(len(ss)))
	for _, s := range ss {
		b = appendString(b, s)
	}
	return b
}

// decoder reads fields in order and remembers the first error, so a
// truncated payload surfaces once instead of at every field.
type decoder struct {
	b   []byte
	err error
}

func (d *decoder) take(n int) []byte {
	if d.err != nil {
		return nil
	}
	if n < 0 || len(d.b) < n {
		d.err = io.ErrUnexpectedEOF
		return nil
	}
	out := d.b[:n]
	d.b = d.b[n:]
	return out
}

func (d *decoder) u32() int {
	b := d.take(4)
	if b == nil {
		return 0
	}
	return int(binary.BigEndian.Uint32(b))
}

func (d *decoder) int() int {
	return int(int32(d.u32()))
}

func (d *decoder) byte() byte {
	b := d.take(1)
	if b == nil {
		return 0
	}
	return b[0]
}

func (d *decoder) string() string {
	return string(d.take(d.u32()))
}

func (d *decoder) strings() []string {
	n := d.u32()
	if d.err != nil || n > len(d.b) {
		d.err = io.ErrUnexpectedEOF
		return nil
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, d.string())
	}
	return out
}
