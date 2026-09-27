// Package rfbclient is a minimal RFB (VNC) protocol client — just enough
// of RFC 6143 to drive wayvnc as a local, unauthenticated pixel/input
// bridge for `tubeless open` (see cmd/tubeless/open.go): version + None
// security handshake, Raw-encoding framebuffer updates, pointer/key
// events, and a best-effort SetDesktopSize request. It deliberately
// doesn't implement encodings, authentication schemes, or clipboard/
// audio extensions this use never needs — the connection never leaves
// localhost and both ends are processes this same project spawns.
package rfbclient

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

// protocolVersion is what this client sends back during the handshake —
// RFB 3.8, the version wayvnc itself reports.
var protocolVersion = [12]byte{'R', 'F', 'B', ' ', '0', '0', '3', '.', '0', '0', '8', '\n'}

// securityNone is the RFB "no authentication" security type — the only
// one offered by a wayvnc instance started with our generated
// enable_auth=false config (see cmd/tubeless/open.go).
const securityNone = 1

// Client is one connection to an RFB server (wayvnc). Not safe for
// concurrent use from multiple goroutines without external
// synchronization — cmd/tubeless/open.go serializes its own reads
// (ReadUpdate) and writes (SendPointerEvent/SendKeyEvent/SetDesktopSize)
// across separate goroutines with its own mutex.
type Client struct {
	conn   net.Conn
	r      *bufio.Reader
	width  int
	height int
	name   string
}

// Dial connects to an RFB server at network/address (e.g. "unix",
// "/path/to/wayvnc.sock") and completes the version/security/init
// handshake, requesting the None security type and a 32bpp true-color
// pixel format laid out as tightly-packed RGBA8 (matching
// pkg/openproto.FrameRect's own expected byte order, so a received Raw
// rectangle's bytes can be forwarded to the window process unmodified).
func Dial(network, address string) (*Client, error) {
	conn, err := net.Dial(network, address)
	if err != nil {
		return nil, fmt.Errorf("rfbclient: dial %s %s: %w", network, address, err)
	}
	c, err := newClient(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return c, nil
}

func newClient(conn net.Conn) (*Client, error) {
	c := &Client{conn: conn, r: bufio.NewReaderSize(conn, 64*1024)}
	if err := c.handshake(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Client) handshake() error {
	var serverVersion [12]byte
	if _, err := io.ReadFull(c.r, serverVersion[:]); err != nil {
		return fmt.Errorf("rfbclient: read server version: %w", err)
	}
	if _, err := c.conn.Write(protocolVersion[:]); err != nil {
		return fmt.Errorf("rfbclient: send client version: %w", err)
	}

	nTypes, err := c.r.ReadByte()
	if err != nil {
		return fmt.Errorf("rfbclient: read security type count: %w", err)
	}
	if nTypes == 0 {
		reason, err := c.readReasonString()
		if err != nil {
			return fmt.Errorf("rfbclient: server refused connection (unreadable reason: %v)", err)
		}
		return fmt.Errorf("rfbclient: server refused connection: %s", reason)
	}
	types := make([]byte, nTypes)
	if _, err := io.ReadFull(c.r, types); err != nil {
		return fmt.Errorf("rfbclient: read security types: %w", err)
	}
	found := false
	for _, t := range types {
		if t == securityNone {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("rfbclient: server didn't offer the None security type (offered %v) — start it with an enable_auth=false config", types)
	}
	if _, err := c.conn.Write([]byte{securityNone}); err != nil {
		return fmt.Errorf("rfbclient: send security type: %w", err)
	}

	var result [4]byte
	if _, err := io.ReadFull(c.r, result[:]); err != nil {
		return fmt.Errorf("rfbclient: read security result: %w", err)
	}
	if binary.BigEndian.Uint32(result[:]) != 0 {
		reason, _ := c.readReasonString()
		return fmt.Errorf("rfbclient: security handshake failed: %s", reason)
	}

	// ClientInit: shared-flag = 1 (share the desktop rather than
	// disconnecting any other viewer — irrelevant here since nothing else
	// ever connects to this wayvnc instance, but 1 is the harmless choice).
	if _, err := c.conn.Write([]byte{1}); err != nil {
		return fmt.Errorf("rfbclient: send ClientInit: %w", err)
	}

	var initHeader [24]byte
	if _, err := io.ReadFull(c.r, initHeader[:]); err != nil {
		return fmt.Errorf("rfbclient: read ServerInit: %w", err)
	}
	c.width = int(binary.BigEndian.Uint16(initHeader[0:2]))
	c.height = int(binary.BigEndian.Uint16(initHeader[2:4]))
	nameLen := binary.BigEndian.Uint32(initHeader[20:24])
	name := make([]byte, nameLen)
	if _, err := io.ReadFull(c.r, name); err != nil {
		return fmt.Errorf("rfbclient: read ServerInit name: %w", err)
	}
	c.name = string(name)

	if err := c.setPixelFormat(); err != nil {
		return err
	}
	return c.setEncodings()
}

func (c *Client) readReasonString() (string, error) {
	var n [4]byte
	if _, err := io.ReadFull(c.r, n[:]); err != nil {
		return "", err
	}
	buf := make([]byte, binary.BigEndian.Uint32(n[:]))
	if _, err := io.ReadFull(c.r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

// pixelFormat requests 32bpp/depth24 true-color, little-endian, laid out
// so the raw bytes of every pixel are exactly R,G,B,<pad> in memory —
// red-shift 0/green-shift 8/blue-shift 16 against a little-endian 32-bit
// read means byte 0 is red, byte 1 green, byte 2 blue. Byte 3 (RFB has no
// alpha channel) is left as whatever the server sends; opaque() below
// overwrites it with opaque alpha before handing pixels on.
func (c *Client) setPixelFormat() error {
	msg := make([]byte, 20)
	msg[0] = 0 // message-type: SetPixelFormat
	// bytes 1:4 padding
	msg[4] = 32                                 // bits-per-pixel
	msg[5] = 24                                 // depth
	msg[6] = 0                                  // big-endian-flag: false
	msg[7] = 1                                  // true-color-flag: true
	binary.BigEndian.PutUint16(msg[8:10], 255)  // red-max
	binary.BigEndian.PutUint16(msg[10:12], 255) // green-max
	binary.BigEndian.PutUint16(msg[12:14], 255) // blue-max
	msg[14] = 0                                 // red-shift
	msg[15] = 8                                 // green-shift
	msg[16] = 16                                // blue-shift
	// bytes 17:20 padding
	if _, err := c.conn.Write(msg); err != nil {
		return fmt.Errorf("rfbclient: send SetPixelFormat: %w", err)
	}
	return nil
}

// Pseudo-encodings this client understands on top of Raw: DesktopSize (a
// bare resize notification with no screen-layout detail) and
// ExtendedDesktopSize (the richer form a SetDesktopSize request's reply
// arrives as, and what real resize support requires — see SetDesktopSize's
// own doc comment on how unverified this path still is).
const (
	encodingRaw            = 0
	encodingDesktopSize    = -223
	encodingExtDesktopSize = -308
)

func (c *Client) setEncodings() error {
	encodings := []int32{encodingRaw, encodingDesktopSize, encodingExtDesktopSize}
	msg := make([]byte, 4+4*len(encodings))
	msg[0] = 2 // message-type: SetEncodings
	// byte 1 padding
	binary.BigEndian.PutUint16(msg[2:4], uint16(len(encodings)))
	for i, e := range encodings {
		binary.BigEndian.PutUint32(msg[4+4*i:8+4*i], uint32(e))
	}
	if _, err := c.conn.Write(msg); err != nil {
		return fmt.Errorf("rfbclient: send SetEncodings: %w", err)
	}
	return nil
}

// Width/Height are the server's current framebuffer dimensions, updated
// by ReadUpdate whenever a resize (DesktopSize/ExtendedDesktopSize)
// rectangle arrives.
func (c *Client) Width() int  { return c.width }
func (c *Client) Height() int { return c.height }

// RequestUpdate asks the server for the next FramebufferUpdate.
// incremental=true asks for only what's changed since the last update
// (the normal steady-state request); false forces a full repaint (used
// once, right after connecting, so the very first frame isn't blank).
func (c *Client) RequestUpdate(incremental bool) error {
	msg := make([]byte, 10)
	msg[0] = 3 // message-type: FramebufferUpdateRequest
	if incremental {
		msg[1] = 1
	}
	binary.BigEndian.PutUint16(msg[2:4], 0)
	binary.BigEndian.PutUint16(msg[4:6], 0)
	binary.BigEndian.PutUint16(msg[6:8], uint16(c.width))
	binary.BigEndian.PutUint16(msg[8:10], uint16(c.height))
	if _, err := c.conn.Write(msg); err != nil {
		return fmt.Errorf("rfbclient: send FramebufferUpdateRequest: %w", err)
	}
	return nil
}

// Update is one parsed rectangle from a FramebufferUpdate message: either
// pixel data (Raw) or a pure resize notification (DesktopSize/
// ExtendedDesktopSize — Pix is nil, Resized is true, W/H are the new
// total framebuffer dimensions).
type Update struct {
	X, Y, W, H int
	Pix        []byte
	Resized    bool
}

// ReadUpdate blocks for the server's next FramebufferUpdate message and
// returns all of its rectangles. An encoding this client didn't advertise
// (see setEncodings) is a protocol error, not something to skip past —
// silently misparsing an unexpected encoding's payload would desync every
// rectangle after it.
func (c *Client) ReadUpdate() ([]Update, error) {
	var header [4]byte
	if _, err := io.ReadFull(c.r, header[:]); err != nil {
		return nil, fmt.Errorf("rfbclient: read message header: %w", err)
	}
	if header[0] != 0 {
		return nil, fmt.Errorf("rfbclient: unexpected server message type %d (only FramebufferUpdate is handled)", header[0])
	}
	nRects := int(binary.BigEndian.Uint16(header[2:4]))
	updates := make([]Update, 0, nRects)
	for i := 0; i < nRects; i++ {
		var rh [12]byte
		if _, err := io.ReadFull(c.r, rh[:]); err != nil {
			return nil, fmt.Errorf("rfbclient: read rectangle header: %w", err)
		}
		x := int(binary.BigEndian.Uint16(rh[0:2]))
		y := int(binary.BigEndian.Uint16(rh[2:4]))
		w := int(binary.BigEndian.Uint16(rh[4:6]))
		h := int(binary.BigEndian.Uint16(rh[6:8]))
		encoding := int32(binary.BigEndian.Uint32(rh[8:12]))

		switch encoding {
		case encodingRaw:
			pix := make([]byte, w*h*4)
			if _, err := io.ReadFull(c.r, pix); err != nil {
				return nil, fmt.Errorf("rfbclient: read Raw rect %dx%d: %w", w, h, err)
			}
			opaque(pix)
			updates = append(updates, Update{X: x, Y: y, W: w, H: h, Pix: pix})
			c.width, c.height = max(c.width, x+w), max(c.height, y+h)
		case encodingDesktopSize:
			c.width, c.height = w, h
			updates = append(updates, Update{W: w, H: h, Resized: true})
		case encodingExtDesktopSize:
			if err := c.skipExtDesktopSize(); err != nil {
				return nil, err
			}
			c.width, c.height = w, h
			updates = append(updates, Update{W: w, H: h, Resized: true})
		default:
			return nil, fmt.Errorf("rfbclient: unadvertised encoding %d in FramebufferUpdate", encoding)
		}
	}
	return updates, nil
}

// skipExtDesktopSize consumes an ExtendedDesktopSize rectangle's payload
// (RFC "Extended Desktop Size" extension): a screen count then that many
// 16-byte screen descriptors. Only the rectangle's own w/h (already read
// by the caller) matter to this client — the per-screen layout is parsed
// only far enough to skip it correctly, not interpreted.
func (c *Client) skipExtDesktopSize() error {
	var count [4]byte
	if _, err := io.ReadFull(c.r, count[:]); err != nil {
		return fmt.Errorf("rfbclient: read ExtendedDesktopSize screen count: %w", err)
	}
	n := int(count[0])
	skip := make([]byte, n*16)
	if _, err := io.ReadFull(c.r, skip); err != nil {
		return fmt.Errorf("rfbclient: read ExtendedDesktopSize screens: %w", err)
	}
	return nil
}

// opaque forces every pixel's 4th byte (RFB's format has no alpha
// channel — see setPixelFormat) to fully opaque, matching what the
// window process's GL texture upload expects for a solid app window.
func opaque(pix []byte) {
	for i := 3; i < len(pix); i += 4 {
		pix[i] = 0xff
	}
}

// Pointer button-mask bits (RFB PointerEvent) — xterm/X11 numbering:
// bit 0 left, bit 1 middle, bit 2 right, bit 3 wheel-up, bit 4
// wheel-down. Callers accumulate these into a running mask (RFB has no
// separate press/release message — every event carries the *complete*
// currently-held set) — see cmd/tubeless/open.go's pointer state.
const (
	ButtonLeft    = 1 << 0
	ButtonMiddle  = 1 << 1
	ButtonRight   = 1 << 2
	ButtonWheelUp = 1 << 3
	ButtonWheelDn = 1 << 4
)

// SendPointerEvent reports the full current button mask at position
// (x, y) in the server's own framebuffer pixel coordinates.
func (c *Client) SendPointerEvent(mask uint8, x, y int) error {
	msg := make([]byte, 6)
	msg[0] = 5 // message-type: PointerEvent
	msg[1] = mask
	binary.BigEndian.PutUint16(msg[2:4], uint16(clampCoord(x)))
	binary.BigEndian.PutUint16(msg[4:6], uint16(clampCoord(y)))
	if _, err := c.conn.Write(msg); err != nil {
		return fmt.Errorf("rfbclient: send PointerEvent: %w", err)
	}
	return nil
}

func clampCoord(v int) int {
	if v < 0 {
		return 0
	}
	if v > 0xffff {
		return 0xffff
	}
	return v
}

// SendKeyEvent sends a single key down/up event for keysym (an X11
// keysym value — see cmd/tubeless's keysym translation).
func (c *Client) SendKeyEvent(keysym uint32, down bool) error {
	msg := make([]byte, 8)
	msg[0] = 4 // message-type: KeyEvent
	if down {
		msg[1] = 1
	}
	// bytes 2:4 padding
	binary.BigEndian.PutUint32(msg[4:8], keysym)
	if _, err := c.conn.Write(msg); err != nil {
		return fmt.Errorf("rfbclient: send KeyEvent: %w", err)
	}
	return nil
}

// SetDesktopSize asks the server to resize its framebuffer to w x h —
// the RFB "SetDesktopSize" client message from the Extended Desktop Size
// extension, sent only because setEncodings above advertised support for
// it. Best-effort: not every RFB server implementation honors this (or
// wayvnc's own behavior on it hasn't been verified live — see the
// project's own gui-app-embedding notes), so callers should log a
// failure here rather than treat it as fatal.
func (c *Client) SetDesktopSize(w, h int) error {
	msg := make([]byte, 8)
	msg[0] = 251 // message-type: SetDesktopSize
	// byte 1 padding
	binary.BigEndian.PutUint16(msg[2:4], uint16(w))
	binary.BigEndian.PutUint16(msg[4:6], uint16(h))
	msg[6] = 1 // number-of-screens: report a single screen
	// byte 7 padding
	screen := make([]byte, 16)
	binary.BigEndian.PutUint32(screen[0:4], 0) // screen id
	binary.BigEndian.PutUint16(screen[4:6], 0) // x-position
	binary.BigEndian.PutUint16(screen[6:8], 0) // y-position
	binary.BigEndian.PutUint16(screen[8:10], uint16(w))
	binary.BigEndian.PutUint16(screen[10:12], uint16(h))
	binary.BigEndian.PutUint32(screen[12:16], 0) // flags
	if _, err := c.conn.Write(append(msg, screen...)); err != nil {
		return fmt.Errorf("rfbclient: send SetDesktopSize: %w", err)
	}
	return nil
}

// Close closes the underlying connection.
func (c *Client) Close() error {
	return c.conn.Close()
}
