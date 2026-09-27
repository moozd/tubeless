package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/moozd/tubeless/pkg/openproto"
)

// openUpdate is what the control-socket read loop hands to the render
// thread for one accepted session's message — either a pixel patch
// (FrameRect) or a pure resize notification (Resize; Pix nil).
type openUpdate struct {
	x, y, w, h int
	pix        []byte
	resize     bool
}

// openSession is the main process's live connection to a running
// `tubeless open`. Exactly one can be active at a time — see
// openServer.handle. wireMouse/wireInput's callbacks call SendInput
// directly (from the GLFW/render thread); the control-socket's own read
// goroutine pushes incoming pixel/resize updates onto updates for the
// render loop to drain (see drainOpenUpdates) — GL calls must only ever
// happen on the render thread, never from this session's read goroutine.
type openSession struct {
	conn      net.Conn
	writeMu   sync.Mutex // serializes SendInput/SendResize against each other and against Close
	updates   chan openUpdate
	done      chan struct{}
	closeOnce sync.Once

	// w/h are the embedded app's current framebuffer pixel size, kept
	// up to date by openServer.handle's read loop independently of
	// updates (mouse.go's pixel-mapping needs to read this from the
	// GLFW callback thread without waiting on a channel drain).
	w, h atomic.Int32
}

func newOpenSession(conn net.Conn) *openSession {
	return &openSession{
		conn:    conn,
		updates: make(chan openUpdate, 64),
		done:    make(chan struct{}),
	}
}

// SendInput forwards ev to the connected `tubeless open` process — called
// from wireMouse/wireInput's GLFW callbacks while this session is active.
func (s *openSession) SendInput(ev openproto.InputEvent) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := openproto.WriteMessage(s.conn, ev); err != nil {
		log.Printf("open session: send input: %v", err)
	}
}

// SendResize asks the embedded app to match the window's own new content
// box size (see wireResize) — best-effort, matching
// pkg/rfbclient.SetDesktopSize's own doc comment on why a failure here
// is logged rather than fatal to the session.
func (s *openSession) SendResize(w, h int) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := openproto.WriteMessage(s.conn, openproto.Resize{W: w, H: h}); err != nil {
		log.Printf("open session: send resize: %v", err)
	}
}

func (s *openSession) close() {
	s.closeOnce.Do(func() {
		s.conn.Close()
		close(s.done)
	})
}

// openServer listens on a per-window Unix control socket (its path is
// what gets exported as TUBELESS_CTL — see ptyio.Start's extraSessionEnv
// and main's own wiring) and accepts `tubeless open` connections one at a
// time: a second OpenRequest while a session is already active is
// rejected outright — v1 takes over the whole terminal, there's no
// notion of multiple embedded apps sharing the grid.
type openServer struct {
	ln     net.Listener
	path   string
	active atomic.Pointer[openSession]
}

// openControlSocketPath is where this process's control socket lives —
// one per window (process), named by PID so a second tubeless window
// never collides with the first's.
func openControlSocketPath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "tubeless", fmt.Sprintf("open-%d.sock", os.Getpid()))
}

// listenOpenServer creates the control socket (parent directories created
// as needed, a stale socket from an unclean previous exit removed first)
// and starts accepting connections in the background.
func listenOpenServer(path string) (*openServer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("open server: create socket dir: %w", err)
	}
	os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("open server: listen %s: %w", path, err)
	}
	s := &openServer{ln: ln, path: path}
	go s.acceptLoop()
	return s, nil
}

// Active returns the currently active session, or nil.
func (s *openServer) Active() *openSession {
	return s.active.Load()
}

// Close stops accepting new connections and tears down any active
// session's socket. Idempotent-safe to call even with no active session.
func (s *openServer) Close() {
	s.ln.Close()
	if sess := s.active.Load(); sess != nil {
		sess.close()
	}
}

func (s *openServer) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return // listener closed at process exit
		}
		go s.handle(conn)
	}
}

// handle runs the entire lifecycle of one `tubeless open` connection:
// the initial OpenRequest/Ack-or-Reject handshake, then a read loop that
// feeds FrameRect/Resize messages to the render thread until the
// connection closes (the app exited, or `tubeless open` itself was
// killed — either way, this is exactly what the plan's fast-teardown
// requirement expects the window side to notice and react to promptly).
func (s *openServer) handle(conn net.Conn) {
	r := openproto.NewReader(conn)
	msg, err := openproto.ReadMessage(r)
	if err != nil {
		log.Printf("open server: read OpenRequest: %v", err)
		conn.Close()
		return
	}
	if _, ok := msg.(openproto.OpenRequest); !ok {
		log.Printf("open server: expected OpenRequest, got %T", msg)
		conn.Close()
		return
	}

	sess := newOpenSession(conn)
	if !s.active.CompareAndSwap(nil, sess) {
		openproto.WriteMessage(conn, openproto.OpenReject{Reason: "a tubeless open session is already active in this window"})
		conn.Close()
		return
	}
	if err := openproto.WriteMessage(conn, openproto.OpenAck{}); err != nil {
		log.Printf("open server: send OpenAck: %v", err)
		s.active.CompareAndSwap(sess, nil)
		conn.Close()
		return
	}

	defer func() {
		s.active.CompareAndSwap(sess, nil)
		sess.close()
	}()

	for {
		msg, err := openproto.ReadMessage(r)
		if err != nil {
			return
		}
		switch m := msg.(type) {
		case openproto.FrameRect:
			select {
			case sess.updates <- openUpdate{x: m.X, y: m.Y, w: m.W, h: m.H, pix: m.Pix}:
			default:
				log.Printf("open server: dropping a FrameRect — render thread isn't keeping up")
			}
		case openproto.Resize:
			sess.w.Store(int32(m.W))
			sess.h.Store(int32(m.H))
			select {
			case sess.updates <- openUpdate{w: m.W, h: m.H, resize: true}:
			default:
			}
		case openproto.Close:
			return
		default:
			log.Printf("open server: unexpected message %T from an active session", msg)
			return
		}
	}
}
