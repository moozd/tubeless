// Package openproto is the private wire protocol between a running
// tubeless window and a `tubeless open <app_cmd>` process embedding a GUI
// app inside it. It never touches the PTY/stdio — the two processes talk
// over a Unix domain control socket (see cmd/tubeless's TUBELESS_CTL env
// var), so this framing is deliberately simple: both ends are the same Go
// binary, never a network peer, so there's no need for a self-describing
// format like protobuf/JSON — a fixed header plus a type-specific manual
// encoding is enough and avoids a new dependency.
package openproto

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
)

// magic distinguishes this protocol's frames from anything else that
// might land on the socket (a stray telnet/curl probe, a version
// mismatch) — ReadMessage refuses to decode a frame that doesn't start
// with it.
var magic = [4]byte{'T', 'B', 'O', 'P'}

// version is bumped on any wire-incompatible change to a message's
// payload encoding. There's exactly one process pair (this build's own
// tubeless and tubeless open), so a mismatch only happens after an
// in-place binary upgrade with a stale child still running — treated as
// fatal by the reader, never silently misdecoded.
const version = 1

// MsgType identifies which message a frame carries.
type MsgType byte

const (
	MsgOpenRequest MsgType = iota + 1
	MsgOpenAck
	MsgOpenReject
	MsgFrameRect
	MsgInputEvent
	MsgResize
	MsgClose
)

func (t MsgType) String() string {
	switch t {
	case MsgOpenRequest:
		return "OpenRequest"
	case MsgOpenAck:
		return "OpenAck"
	case MsgOpenReject:
		return "OpenReject"
	case MsgFrameRect:
		return "FrameRect"
	case MsgInputEvent:
		return "InputEvent"
	case MsgResize:
		return "Resize"
	case MsgClose:
		return "Close"
	default:
		return fmt.Sprintf("MsgType(%d)", byte(t))
	}
}

// maxPayload bounds a single frame's payload — generous enough for a
// full-window FrameRect at a large 4K-class window (see FrameRect's own
// doc comment on its size) while still refusing an absurd/corrupt length
// prefix outright instead of trying to allocate it.
const maxPayload = 64 << 20 // 64 MiB

// OpenRequest is sent open->window to ask for the takeover: title is the
// window title to show while the app is embedded (main.go's windowTitle
// convention — empty falls back to the plain app name unchanged).
type OpenRequest struct {
	Title string
}

// OpenAck is sent window->open once the takeover is accepted and active.
type OpenAck struct{}

// OpenReject is sent window->open when a takeover can't start (a session
// is already active, or the request is otherwise invalid) — Reason is a
// human-readable message tubeless open should print and exit on.
type OpenReject struct {
	Reason string
}

// FrameRect is a damaged-region pixel update, open->window: (X,Y) is the
// top-left corner and Pix is exactly W*H*4 bytes of tightly-packed RGBA8,
// row-major, matching what the renderer's OpenTexture upload expects
// (mirrors RFB's own FramebufferUpdate rects, just re-encoded into this
// framing instead of forwarding raw RFB bytes over the control socket).
type FrameRect struct {
	X, Y, W, H int
	Pix        []byte
}

// InputKind identifies what an InputEvent describes.
type InputKind byte

const (
	InputPointerMove InputKind = iota + 1
	InputPointerButton
	InputPointerScroll
	InputKey
)

// PointerButton mirrors X11/xterm's left/middle/right numbering (the same
// convention pkg/screen's mouse reporting already uses), not GLFW's.
type PointerButton byte

const (
	PointerNone PointerButton = iota
	PointerLeft
	PointerMiddle
	PointerRight
)

// KeyCode names a non-printable key (or a modifier key's own press/
// release) using its X11 keysym value directly — pkg/rfbclient's KeyEvent
// encoding needs an X11 keysym regardless, so there's no separate
// tubeless-specific enum to translate through. 0 means "not a special
// key" (InputEvent.Rune carries a printable character instead).
type KeyCode uint32

// The subset of X11 keysyms (from X11/keysymdef.h) this protocol forwards
// — every key cmd/tubeless's own input.go already special-cases
// (pkg/ptyio never needs to know about any of this; these are the
// embedded app's keys, not the shell's), plus the four modifier keys
// RFB's KeyEvent needs bracketed around any other key (see
// cmd/tubeless/open.go's keysym translation).
const (
	KeyBackspace KeyCode = 0xff08
	KeyTab       KeyCode = 0xff09
	KeyReturn    KeyCode = 0xff0d
	KeyEscape    KeyCode = 0xff1b
	KeyDelete    KeyCode = 0xffff

	KeyHome     KeyCode = 0xff50
	KeyLeft     KeyCode = 0xff51
	KeyUp       KeyCode = 0xff52
	KeyRight    KeyCode = 0xff53
	KeyDown     KeyCode = 0xff54
	KeyPageUp   KeyCode = 0xff55
	KeyPageDown KeyCode = 0xff56
	KeyEnd      KeyCode = 0xff57
	KeyInsert   KeyCode = 0xff63

	KeyF1  KeyCode = 0xffbe
	KeyF2  KeyCode = 0xffbf
	KeyF3  KeyCode = 0xffc0
	KeyF4  KeyCode = 0xffc1
	KeyF5  KeyCode = 0xffc2
	KeyF6  KeyCode = 0xffc3
	KeyF7  KeyCode = 0xffc4
	KeyF8  KeyCode = 0xffc5
	KeyF9  KeyCode = 0xffc6
	KeyF10 KeyCode = 0xffc7
	KeyF11 KeyCode = 0xffc8
	KeyF12 KeyCode = 0xffc9

	KeyShiftL   KeyCode = 0xffe1
	KeyShiftR   KeyCode = 0xffe2
	KeyControlL KeyCode = 0xffe3
	KeyControlR KeyCode = 0xffe4
	KeyAltL     KeyCode = 0xffe9
	KeyAltR     KeyCode = 0xffea
	KeySuperL   KeyCode = 0xffeb
	KeySuperR   KeyCode = 0xffec
)

// InputEvent is a single pointer or key event, window->open, forwarded on
// to wayvnc as an RFB PointerEvent/KeyEvent (see cmd/tubeless/open.go). X/Y
// on a pointer event are already scaled into the embedded app's own pixel
// framebuffer, not the terminal's cell grid or the local window's raw
// pixels — the window process does that scaling before sending (see
// mouse.go's openPixelFromFramebufferPixels).
type InputEvent struct {
	Kind    InputKind
	X, Y    int
	Button  PointerButton
	Pressed bool // press/down = true, release/up = false
	Scroll  int  // notches; positive = up, negative = down
	Code    KeyCode
	Rune    rune // printable character; 0 when Code names a non-printable key
}

// Resize is sent window->open when the local window's content box
// changes size (a live resize, a font/DPI change) — W/H are the new
// target pixel dimensions the embedded app's own framebuffer should
// match, best-effort (see cmd/tubeless/open.go's SetDesktopSize request;
// not every VNC server implementation actually resizes on this).
type Resize struct {
	W, H int
}

// Close is sent in either direction as a clean-teardown signal — Reason
// is logged by the receiver, never required to be non-empty.
type Close struct {
	Reason string
}

// WriteMessage frames and writes msg (one of the payload types above) to
// w. Safe to call concurrently with reads on the same connection, but not
// with another concurrent WriteMessage — callers needing that must
// serialize their own writes (see cmd/tubeless's openSession, which holds
// a mutex around this).
func WriteMessage(w io.Writer, msg any) error {
	t, payload, err := encode(msg)
	if err != nil {
		return err
	}
	if len(payload) > maxPayload {
		return fmt.Errorf("openproto: encode %s: payload %d bytes exceeds max %d", t, len(payload), maxPayload)
	}
	header := make([]byte, 10)
	copy(header[0:4], magic[:])
	header[4] = version
	header[5] = byte(t)
	binary.BigEndian.PutUint32(header[6:10], uint32(len(payload)))
	if _, err := w.Write(header); err != nil {
		return fmt.Errorf("openproto: write %s header: %w", t, err)
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return fmt.Errorf("openproto: write %s payload: %w", t, err)
		}
	}
	return nil
}

// ReadMessage reads and decodes the next frame from r, returning one of
// the payload types above as `any` — callers type-switch on the result.
// r should be buffered (see NewReader) since this issues several small
// reads per frame.
func ReadMessage(r io.Reader) (any, error) {
	var header [10]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	if header[0] != magic[0] || header[1] != magic[1] || header[2] != magic[2] || header[3] != magic[3] {
		return nil, fmt.Errorf("openproto: bad magic %x", header[0:4])
	}
	if header[4] != version {
		return nil, fmt.Errorf("openproto: unsupported version %d (want %d)", header[4], version)
	}
	t := MsgType(header[5])
	n := binary.BigEndian.Uint32(header[6:10])
	if n > maxPayload {
		return nil, fmt.Errorf("openproto: %s payload %d bytes exceeds max %d", t, n, maxPayload)
	}
	payload := make([]byte, n)
	if n > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, fmt.Errorf("openproto: read %s payload: %w", t, err)
		}
	}
	return decode(t, payload)
}

// NewReader wraps r for ReadMessage's small repeated reads — a plain
// net.Conn/os.File does one syscall per Read, and a single frame issues
// several (header, then payload), so this matters for real socket
// traffic even though it's invisible in the in-memory pipe tests use.
func NewReader(r io.Reader) *bufio.Reader {
	return bufio.NewReaderSize(r, 32*1024)
}

func encode(msg any) (MsgType, []byte, error) {
	switch m := msg.(type) {
	case OpenRequest:
		return MsgOpenRequest, encodeOpenRequest(m), nil
	case OpenAck:
		return MsgOpenAck, nil, nil
	case OpenReject:
		return MsgOpenReject, encodeOpenReject(m), nil
	case FrameRect:
		return MsgFrameRect, encodeFrameRect(m), nil
	case InputEvent:
		return MsgInputEvent, encodeInputEvent(m), nil
	case Resize:
		return MsgResize, encodeResize(m), nil
	case Close:
		return MsgClose, encodeClose(m), nil
	default:
		return 0, nil, fmt.Errorf("openproto: encode: unknown message type %T", msg)
	}
}

func decode(t MsgType, payload []byte) (any, error) {
	switch t {
	case MsgOpenRequest:
		return decodeOpenRequest(payload)
	case MsgOpenAck:
		return OpenAck{}, nil
	case MsgOpenReject:
		return decodeOpenReject(payload)
	case MsgFrameRect:
		return decodeFrameRect(payload)
	case MsgInputEvent:
		return decodeInputEvent(payload)
	case MsgResize:
		return decodeResize(payload)
	case MsgClose:
		return decodeClose(payload)
	default:
		return nil, fmt.Errorf("openproto: decode: unknown message type %s", t)
	}
}

// --- string helpers (length-prefixed uint16, plenty for a window title
// or an error message) ---

func appendString(b []byte, s string) []byte {
	var n [2]byte
	binary.BigEndian.PutUint16(n[:], uint16(len(s)))
	b = append(b, n[:]...)
	return append(b, s...)
}

func readString(b []byte) (s string, rest []byte, err error) {
	if len(b) < 2 {
		return "", nil, fmt.Errorf("openproto: truncated string length")
	}
	n := int(binary.BigEndian.Uint16(b[0:2]))
	b = b[2:]
	if len(b) < n {
		return "", nil, fmt.Errorf("openproto: truncated string body")
	}
	return string(b[:n]), b[n:], nil
}

func encodeOpenRequest(m OpenRequest) []byte {
	return appendString(nil, m.Title)
}

func decodeOpenRequest(b []byte) (OpenRequest, error) {
	title, _, err := readString(b)
	if err != nil {
		return OpenRequest{}, err
	}
	return OpenRequest{Title: title}, nil
}

func encodeOpenReject(m OpenReject) []byte {
	return appendString(nil, m.Reason)
}

func decodeOpenReject(b []byte) (OpenReject, error) {
	reason, _, err := readString(b)
	if err != nil {
		return OpenReject{}, err
	}
	return OpenReject{Reason: reason}, nil
}

func encodeClose(m Close) []byte {
	return appendString(nil, m.Reason)
}

func decodeClose(b []byte) (Close, error) {
	reason, _, err := readString(b)
	if err != nil {
		return Close{}, err
	}
	return Close{Reason: reason}, nil
}

func encodeResize(m Resize) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint32(b[0:4], uint32(m.W))
	binary.BigEndian.PutUint32(b[4:8], uint32(m.H))
	return b
}

func decodeResize(b []byte) (Resize, error) {
	if len(b) < 8 {
		return Resize{}, fmt.Errorf("openproto: truncated Resize")
	}
	return Resize{
		W: int(binary.BigEndian.Uint32(b[0:4])),
		H: int(binary.BigEndian.Uint32(b[4:8])),
	}, nil
}

// encodeFrameRect lays out X,Y,W,H (int32 each) then W*H*4 raw pixel
// bytes — no separate length prefix for Pix since W*H*4 already
// determines it exactly.
func encodeFrameRect(m FrameRect) []byte {
	b := make([]byte, 16+len(m.Pix))
	binary.BigEndian.PutUint32(b[0:4], uint32(m.X))
	binary.BigEndian.PutUint32(b[4:8], uint32(m.Y))
	binary.BigEndian.PutUint32(b[8:12], uint32(m.W))
	binary.BigEndian.PutUint32(b[12:16], uint32(m.H))
	copy(b[16:], m.Pix)
	return b
}

func decodeFrameRect(b []byte) (FrameRect, error) {
	if len(b) < 16 {
		return FrameRect{}, fmt.Errorf("openproto: truncated FrameRect header")
	}
	x := int(binary.BigEndian.Uint32(b[0:4]))
	y := int(binary.BigEndian.Uint32(b[4:8]))
	w := int(binary.BigEndian.Uint32(b[8:12]))
	h := int(binary.BigEndian.Uint32(b[12:16]))
	want := w * h * 4
	if want < 0 || len(b)-16 != want {
		return FrameRect{}, fmt.Errorf("openproto: FrameRect %dx%d wants %d pixel bytes, got %d", w, h, want, len(b)-16)
	}
	pix := make([]byte, want)
	copy(pix, b[16:])
	return FrameRect{X: x, Y: y, W: w, H: h, Pix: pix}, nil
}

// encodeInputEvent: kind(1) + button(1) + pressed(1) + pad(1) + x(4) +
// y(4) + scroll(4) + code(4) + rune(4). Fixed-width and generous rather
// than variant-packed — an InputEvent is tiny next to a FrameRect and
// there's no benefit to shaving a few bytes off the common case.
func encodeInputEvent(m InputEvent) []byte {
	b := make([]byte, 24)
	b[0] = byte(m.Kind)
	b[1] = byte(m.Button)
	if m.Pressed {
		b[2] = 1
	}
	binary.BigEndian.PutUint32(b[4:8], uint32(int32(m.X)))
	binary.BigEndian.PutUint32(b[8:12], uint32(int32(m.Y)))
	binary.BigEndian.PutUint32(b[12:16], uint32(int32(m.Scroll)))
	binary.BigEndian.PutUint32(b[16:20], uint32(m.Code))
	binary.BigEndian.PutUint32(b[20:24], uint32(m.Rune))
	return b
}

func decodeInputEvent(b []byte) (InputEvent, error) {
	if len(b) < 24 {
		return InputEvent{}, fmt.Errorf("openproto: truncated InputEvent")
	}
	return InputEvent{
		Kind:    InputKind(b[0]),
		Button:  PointerButton(b[1]),
		Pressed: b[2] != 0,
		X:       int(int32(binary.BigEndian.Uint32(b[4:8]))),
		Y:       int(int32(binary.BigEndian.Uint32(b[8:12]))),
		Scroll:  int(int32(binary.BigEndian.Uint32(b[12:16]))),
		Code:    KeyCode(binary.BigEndian.Uint32(b[16:20])),
		Rune:    rune(binary.BigEndian.Uint32(b[20:24])),
	}, nil
}
