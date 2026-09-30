// Package wmproto is the wire protocol between the tubeless window and
// a tubeless-wm compositor process (see compositor/src/proto.rs, its
// mirror). One SOCK_SEQPACKET message per packet: a type byte followed
// by little-endian fields. Frame pixels never travel in a message: the
// compositor sends a memfd once (Buffer, with SCM_RIGHTS) and each
// Frame names the damaged rect inside it.
package wmproto

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"
)

const Version = 1

const maxPacket = 4096

const (
	typeResize        = 0x01
	typePointerMove   = 0x02
	typePointerButton = 0x03
	typePointerAxis   = 0x04
	typeKey           = 0x05
	typeFocus         = 0x06

	typeHello  = 0x81
	typeBuffer = 0x82
	typeFrame  = 0x83
	typeExited = 0x85
)

// Messages the window sends to the compositor.
type (
	Resize        struct{ W, H int }
	PointerMove   struct{ X, Y int }
	PointerButton struct {
		Button  uint8 // 1 left, 2 middle, 3 right
		Pressed bool
	}
	PointerAxis struct{ DX, DY int } // wheel notches; positive DY scrolls up
	Key         struct {
		Keysym  uint32
		Pressed bool
	}
	Focus struct{ Focused bool }
)

// Messages the compositor sends to the window.
type (
	Hello struct{ Version uint8 }

	// Buffer announces a new shared frame buffer of BGRA pixels. FD is a
	// memfd the receiver owns and must close once it has mapped it.
	Buffer struct {
		W, H, Stride int
		FD           int
	}
	Frame  struct{ X, Y, W, H int }
	Exited struct{ Code int }
)

func put32(b []byte, v int) []byte {
	return binary.LittleEndian.AppendUint32(b, uint32(int32(v)))
}

func bool8(v bool) byte {
	if v {
		return 1
	}
	return 0
}

func encode(msg any) ([]byte, error) {
	switch m := msg.(type) {
	case Resize:
		return put32(put32([]byte{typeResize}, m.W), m.H), nil
	case PointerMove:
		return put32(put32([]byte{typePointerMove}, m.X), m.Y), nil
	case PointerButton:
		return []byte{typePointerButton, m.Button, bool8(m.Pressed)}, nil
	case PointerAxis:
		return put32(put32([]byte{typePointerAxis}, m.DX), m.DY), nil
	case Key:
		b := binary.LittleEndian.AppendUint32([]byte{typeKey}, m.Keysym)
		return append(b, bool8(m.Pressed)), nil
	case Focus:
		return []byte{typeFocus, bool8(m.Focused)}, nil
	case Hello:
		return []byte{typeHello, m.Version}, nil
	case Buffer:
		return put32(put32(put32([]byte{typeBuffer}, m.W), m.H), m.Stride), nil
	case Frame:
		return put32(put32(put32(put32([]byte{typeFrame}, m.X), m.Y), m.W), m.H), nil
	case Exited:
		return put32([]byte{typeExited}, m.Code), nil
	}
	return nil, fmt.Errorf("wmproto: cannot encode %T", msg)
}

func get32(b []byte, i int) (int, error) {
	if len(b) < i+4 {
		return 0, fmt.Errorf("wmproto: truncated message")
	}
	return int(int32(binary.LittleEndian.Uint32(b[i:]))), nil
}

func decode(b []byte) (any, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("wmproto: empty message")
	}
	switch b[0] {
	case typeResize:
		w, err := get32(b, 1)
		h, err2 := get32(b, 5)
		return Resize{w, h}, firstErr(err, err2)
	case typePointerMove:
		x, err := get32(b, 1)
		y, err2 := get32(b, 5)
		return PointerMove{x, y}, firstErr(err, err2)
	case typePointerButton:
		if len(b) < 3 {
			return nil, fmt.Errorf("wmproto: truncated PointerButton")
		}
		return PointerButton{b[1], b[2] != 0}, nil
	case typePointerAxis:
		x, err := get32(b, 1)
		y, err2 := get32(b, 5)
		return PointerAxis{x, y}, firstErr(err, err2)
	case typeKey:
		if len(b) < 6 {
			return nil, fmt.Errorf("wmproto: truncated Key")
		}
		return Key{binary.LittleEndian.Uint32(b[1:]), b[5] != 0}, nil
	case typeFocus:
		if len(b) < 2 {
			return nil, fmt.Errorf("wmproto: truncated Focus")
		}
		return Focus{b[1] != 0}, nil
	case typeHello:
		if len(b) < 2 {
			return nil, fmt.Errorf("wmproto: truncated Hello")
		}
		return Hello{b[1]}, nil
	case typeBuffer:
		w, e1 := get32(b, 1)
		h, e2 := get32(b, 5)
		s, e3 := get32(b, 9)
		return Buffer{W: w, H: h, Stride: s, FD: -1}, firstErr(e1, e2, e3)
	case typeFrame:
		x, e1 := get32(b, 1)
		y, e2 := get32(b, 5)
		w, e3 := get32(b, 9)
		h, e4 := get32(b, 13)
		return Frame{x, y, w, h}, firstErr(e1, e2, e3, e4)
	case typeExited:
		c, err := get32(b, 1)
		return Exited{c}, err
	}
	return nil, fmt.Errorf("wmproto: unknown message type %#x", b[0])
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// Conn is one end of a wmproto socket. Send is safe for concurrent use;
// Recv must be called from a single goroutine.
type Conn struct {
	c  *net.UnixConn
	mu sync.Mutex
}

// NewConn wraps a connected SOCK_SEQPACKET unix socket file.
func NewConn(f *os.File) (*Conn, error) {
	fc, err := net.FileConn(f)
	if err != nil {
		return nil, fmt.Errorf("wmproto: wrap socket: %w", err)
	}
	uc, ok := fc.(*net.UnixConn)
	if !ok {
		fc.Close()
		return nil, fmt.Errorf("wmproto: socket is %T, want *net.UnixConn", fc)
	}
	return &Conn{c: uc}, nil
}

// Send writes one message. A Buffer also carries its FD via SCM_RIGHTS.
func (c *Conn) Send(msg any) error {
	b, err := encode(msg)
	if err != nil {
		return err
	}
	var oob []byte
	if buf, ok := msg.(Buffer); ok {
		oob = syscall.UnixRights(buf.FD)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, _, err := c.c.WriteMsgUnix(b, oob, nil); err != nil {
		return fmt.Errorf("wmproto: send %T: %w", msg, err)
	}
	return nil
}

// Recv blocks for the next message. A returned Buffer owns its FD.
func (c *Conn) Recv() (any, error) {
	buf := make([]byte, maxPacket)
	oob := make([]byte, syscall.CmsgSpace(4))
	n, oobn, _, _, err := c.c.ReadMsgUnix(buf, oob)
	if err != nil {
		return nil, fmt.Errorf("wmproto: receive: %w", err)
	}
	if n == 0 {
		return nil, fmt.Errorf("wmproto: receive: %w", net.ErrClosed)
	}
	msg, err := decode(buf[:n])
	if err != nil {
		return nil, err
	}
	bufMsg, isBuffer := msg.(Buffer)
	if !isBuffer {
		return msg, nil
	}
	fd, err := parseFD(oob[:oobn])
	if err != nil {
		return nil, err
	}
	bufMsg.FD = fd
	return bufMsg, nil
}

func parseFD(oob []byte) (int, error) {
	cms, err := syscall.ParseSocketControlMessage(oob)
	if err != nil {
		return -1, fmt.Errorf("wmproto: parse control message: %w", err)
	}
	for _, cm := range cms {
		fds, err := syscall.ParseUnixRights(&cm)
		if err == nil && len(fds) > 0 {
			return fds[0], nil
		}
	}
	return -1, fmt.Errorf("wmproto: Buffer arrived without a file descriptor")
}

func (c *Conn) Close() error {
	return c.c.Close()
}
