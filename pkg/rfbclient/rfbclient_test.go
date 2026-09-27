package rfbclient

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

// fakeServer drives the server side of the handshake over conn well
// enough to exercise Dial/ReadUpdate/Send* against a real byte stream,
// without needing an actual wayvnc process. It runs in its own goroutine
// and reports any protocol violation on errCh.
type fakeServer struct {
	conn   net.Conn
	errCh  chan error
	client [][]byte // raw bytes read past the handshake, one per Read
}

func startFakeServer(t *testing.T, conn net.Conn) *fakeServer {
	t.Helper()
	s := &fakeServer{conn: conn, errCh: make(chan error, 1)}
	go func() {
		s.errCh <- s.run()
	}()
	return s
}

func (s *fakeServer) run() error {
	if _, err := s.conn.Write([]byte("RFB 003.008\n")); err != nil {
		return err
	}
	var clientVersion [12]byte
	if _, err := io.ReadFull(s.conn, clientVersion[:]); err != nil {
		return err
	}
	// Offer only security type 1 (None).
	if _, err := s.conn.Write([]byte{1, 1}); err != nil {
		return err
	}
	var chosen [1]byte
	if _, err := io.ReadFull(s.conn, chosen[:]); err != nil {
		return err
	}
	if chosen[0] != securityNone {
		return errUnexpected("chosen security type", chosen[0])
	}
	// SecurityResult: OK.
	if _, err := s.conn.Write([]byte{0, 0, 0, 0}); err != nil {
		return err
	}
	var shared [1]byte
	if _, err := io.ReadFull(s.conn, shared[:]); err != nil {
		return err
	}
	// ServerInit: 800x600, a 16-byte pixel format placeholder (client
	// overrides it via SetPixelFormat right after), empty name.
	init := make([]byte, 24)
	binary.BigEndian.PutUint16(init[0:2], 800)
	binary.BigEndian.PutUint16(init[2:4], 600)
	if _, err := s.conn.Write(init); err != nil {
		return err
	}

	// SetPixelFormat (20 bytes) then SetEncodings (header + N*4 bytes).
	pf := make([]byte, 20)
	if _, err := io.ReadFull(s.conn, pf); err != nil {
		return err
	}
	if pf[0] != 0 {
		return errUnexpected("SetPixelFormat message type", pf[0])
	}
	var encHeader [4]byte
	if _, err := io.ReadFull(s.conn, encHeader[:]); err != nil {
		return err
	}
	if encHeader[0] != 2 {
		return errUnexpected("SetEncodings message type", encHeader[0])
	}
	nEnc := binary.BigEndian.Uint16(encHeader[2:4])
	encBody := make([]byte, int(nEnc)*4)
	if _, err := io.ReadFull(s.conn, encBody); err != nil {
		return err
	}
	return nil
}

func errUnexpected(what string, got byte) error {
	return fmt.Errorf("%s: unexpected value %d", what, got)
}

func dialFakeServer(t *testing.T) (*Client, *fakeServer) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	srv := startFakeServer(t, serverConn)

	type result struct {
		c   *Client
		err error
	}
	resCh := make(chan result, 1)
	go func() {
		c, err := newClient(clientConn)
		resCh <- result{c, err}
	}()

	select {
	case res := <-resCh:
		if res.err != nil {
			t.Fatalf("newClient: %v", res.err)
		}
		select {
		case err := <-srv.errCh:
			if err != nil {
				t.Fatalf("fake server: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatalf("fake server didn't finish the handshake in time")
		}
		return res.c, srv
	case <-time.After(2 * time.Second):
		t.Fatalf("handshake didn't complete in time")
		return nil, nil
	}
}

func TestHandshakeAndDimensions(t *testing.T) {
	c, _ := dialFakeServer(t)
	defer c.Close()
	if c.Width() != 800 || c.Height() != 600 {
		t.Fatalf("got %dx%d, want 800x600", c.Width(), c.Height())
	}
}

func TestReadUpdateRaw(t *testing.T) {
	c, srv := dialFakeServer(t)
	defer c.Close()

	go func() {
		// One FramebufferUpdate with a single 2x2 Raw rectangle.
		var msg [4]byte
		msg[2] = 0
		msg[3] = 1 // 1 rectangle
		srv.conn.Write(msg[:])

		rect := make([]byte, 12)
		binary.BigEndian.PutUint16(rect[0:2], 3)  // x
		binary.BigEndian.PutUint16(rect[2:4], 5)  // y
		binary.BigEndian.PutUint16(rect[4:6], 2)  // w
		binary.BigEndian.PutUint16(rect[6:8], 2)  // h
		binary.BigEndian.PutUint32(rect[8:12], 0) // encoding: Raw
		srv.conn.Write(rect)
		srv.conn.Write(make([]byte, 2*2*4)) // pixel data
	}()

	updates, err := c.ReadUpdate()
	if err != nil {
		t.Fatalf("ReadUpdate: %v", err)
	}
	if len(updates) != 1 {
		t.Fatalf("got %d updates, want 1", len(updates))
	}
	u := updates[0]
	if u.X != 3 || u.Y != 5 || u.W != 2 || u.H != 2 {
		t.Fatalf("got geometry %+v", u)
	}
	if len(u.Pix) != 2*2*4 {
		t.Fatalf("got %d pixel bytes, want 16", len(u.Pix))
	}
	for i := 3; i < len(u.Pix); i += 4 {
		if u.Pix[i] != 0xff {
			t.Fatalf("alpha byte at %d = %#x, want 0xff (forced opaque)", i, u.Pix[i])
		}
	}
}

func TestReadUpdateDesktopSize(t *testing.T) {
	c, srv := dialFakeServer(t)
	defer c.Close()

	go func() {
		var msg [4]byte
		msg[3] = 1
		srv.conn.Write(msg[:])

		rect := make([]byte, 12)
		binary.BigEndian.PutUint16(rect[4:6], 1024)
		binary.BigEndian.PutUint16(rect[6:8], 768)
		enc := int32(encodingDesktopSize)
		binary.BigEndian.PutUint32(rect[8:12], uint32(enc))
		srv.conn.Write(rect)
	}()

	updates, err := c.ReadUpdate()
	if err != nil {
		t.Fatalf("ReadUpdate: %v", err)
	}
	if len(updates) != 1 || !updates[0].Resized {
		t.Fatalf("got %+v, want one Resized update", updates)
	}
	if c.Width() != 1024 || c.Height() != 768 {
		t.Fatalf("client dimensions not updated: got %dx%d", c.Width(), c.Height())
	}
}

func TestReadUpdateUnadvertisedEncodingErrors(t *testing.T) {
	c, srv := dialFakeServer(t)
	defer c.Close()

	go func() {
		var msg [4]byte
		msg[3] = 1
		srv.conn.Write(msg[:])
		rect := make([]byte, 12)
		binary.BigEndian.PutUint32(rect[8:12], 99) // encoding no client ever advertises
		srv.conn.Write(rect)
	}()

	if _, err := c.ReadUpdate(); err == nil {
		t.Fatalf("expected an error for an unadvertised encoding")
	}
}

func TestSendPointerAndKeyEvents(t *testing.T) {
	c, srv := dialFakeServer(t)
	defer c.Close()

	readCh := make(chan []byte, 3)
	go func() {
		for i := 0; i < 3; i++ {
			buf := make([]byte, 8)
			n, err := srv.conn.Read(buf)
			if err != nil {
				return
			}
			readCh <- buf[:n]
		}
	}()

	if err := c.SendPointerEvent(ButtonLeft, 10, 20); err != nil {
		t.Fatalf("SendPointerEvent: %v", err)
	}
	if err := c.SendKeyEvent(0x61, true); err != nil {
		t.Fatalf("SendKeyEvent down: %v", err)
	}
	if err := c.SendKeyEvent(0x61, false); err != nil {
		t.Fatalf("SendKeyEvent up: %v", err)
	}

	timeout := time.After(2 * time.Second)
	var msgs [][]byte
	for len(msgs) < 3 {
		select {
		case m := <-readCh:
			msgs = append(msgs, m)
		case <-timeout:
			t.Fatalf("only received %d of 3 messages", len(msgs))
		}
	}

	ptr := msgs[0]
	if ptr[0] != 5 || ptr[1] != ButtonLeft {
		t.Fatalf("PointerEvent header = %v", ptr[:2])
	}
	if x := binary.BigEndian.Uint16(ptr[2:4]); x != 10 {
		t.Fatalf("PointerEvent x = %d, want 10", x)
	}

	keyDown := msgs[1]
	if keyDown[0] != 4 || keyDown[1] != 1 {
		t.Fatalf("KeyEvent down header = %v", keyDown[:2])
	}
	if ks := binary.BigEndian.Uint32(keyDown[4:8]); ks != 0x61 {
		t.Fatalf("KeyEvent keysym = %#x, want 0x61", ks)
	}

	keyUp := msgs[2]
	if keyUp[0] != 4 || keyUp[1] != 0 {
		t.Fatalf("KeyEvent up header = %v", keyUp[:2])
	}
}
