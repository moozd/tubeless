package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/moozd/tubeless/pkg/openproto"
	"github.com/moozd/tubeless/pkg/procgroup"
	"github.com/moozd/tubeless/pkg/render"
	"github.com/moozd/tubeless/pkg/screen"
	"github.com/moozd/tubeless/pkg/wmproto"
)

const (
	slotCloseGrace = 2 * time.Second
	wmBinaryName   = "tubeless-wm"
)

// slotHub owns every embedded GUI app in this window. A `tubeless open`
// process inside a tmux pane asks for a slot over the control socket;
// the hub runs the app under its own tubeless-wm compositor and then
// keeps the app's texture painted over whichever cells of the terminal
// grid still show that pane (see scan), so tmux stays in charge of
// layout, focus, zoom and popups.
type slotHub struct {
	ln       net.Listener
	path     string
	wmPath   string
	cellPx   atomic.Pointer[[2]float32]
	mu       sync.Mutex
	slots    map[int]*slot
	released []int // ids whose textures the render thread should drop
	ttyLinks []string

	layout *slotLayout // render thread only
}

// slot is one running app. Fields below mu are shared between the
// goroutines that talk to the pane process and compositor and the render
// thread; geometry fields are render-thread only.
type slot struct {
	id     int
	client net.Conn
	wm     *wmproto.Conn
	proc   *procgroup.Watched

	clientMu  sync.Mutex // serializes writes to client
	closeOnce sync.Once

	mu         sync.Mutex
	cols, rows int // the pane's size in cells, as the pane process reports
	frame      []byte
	fw, fh     int
	stride     int
	gen        uint64
	damage     damageRect

	uploadedGen uint64
	sentW       int
	sentH       int
	anchorCol   int
	anchorRow   int
	haveAnchor  bool
}

type damageRect struct {
	x0, y0, x1, y1 int
	set            bool
}

func (d *damageRect) add(x, y, w, h int) {
	if !d.set {
		*d = damageRect{x, y, x + w, y + h, true}
		return
	}
	d.x0, d.y0 = min(d.x0, x), min(d.y0, y)
	d.x1, d.y1 = max(d.x1, x+w), max(d.y1, y+h)
}

func slotControlSocketPath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "tubeless", fmt.Sprintf("open-%d.sock", os.Getpid()))
}

// findWMBinary looks for tubeless-wm next to this executable first (an
// installed or freshly built tree), then on PATH.
func findWMBinary() (string, error) {
	if exe, err := os.Executable(); err == nil {
		beside := filepath.Join(filepath.Dir(exe), wmBinaryName)
		if _, err := os.Stat(beside); err == nil {
			return beside, nil
		}
	}
	p, err := exec.LookPath(wmBinaryName)
	if err != nil {
		return "", fmt.Errorf("%s not found next to tubeless or on PATH", wmBinaryName)
	}
	return p, nil
}

// listenSlotHub creates the control socket (removing a stale one left by
// an unclean exit) and starts accepting pane processes in the background.
func listenSlotHub(path string) (*slotHub, error) {
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("GUI apps in panes are Linux only")
	}
	wmPath, err := findWMBinary()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("slot hub: create socket dir: %w", err)
	}
	os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("slot hub: listen %s: %w", path, err)
	}
	h := &slotHub{ln: ln, path: path, wmPath: wmPath, slots: make(map[int]*slot)}
	go h.acceptLoop()
	return h, nil
}

// ttyLinkName is the file a pane process finds its window by: tmux
// reports the attached client's tty, which is this window's pty slave.
func ttyLinkName(tty string) string {
	return strings.ReplaceAll(strings.TrimPrefix(tty, "/dev/"), "/", "_") + ".sock"
}

// registerTTY makes this window findable from inside tmux by the pty it
// is attached through. A tmux server can outlive the window that started
// it, so the inherited $TUBELESS_CTL may name a dead window; the tty
// always names the live one.
func (h *slotHub) registerTTY(tty string) {
	if h == nil || tty == "" {
		return
	}
	link := filepath.Join(filepath.Dir(h.path), "tty", ttyLinkName(tty))
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		log.Printf("slot hub: create tty dir: %v", err)
		return
	}
	os.Remove(link)
	if err := os.Symlink(h.path, link); err != nil {
		log.Printf("slot hub: register %s: %v", tty, err)
		return
	}
	h.mu.Lock()
	h.ttyLinks = append(h.ttyLinks, link)
	h.mu.Unlock()
}

func (h *slotHub) Close() {
	h.ln.Close()
	os.Remove(h.path)
	h.mu.Lock()
	links := h.ttyLinks
	slots := make([]*slot, 0, len(h.slots))
	for _, s := range h.slots {
		slots = append(slots, s)
	}
	h.mu.Unlock()
	for _, l := range links {
		os.Remove(l)
	}
	for _, s := range slots {
		s.close(h)
	}
}

// SetCellSize tells the hub how big a cell is in window pixels; the
// render thread calls it every frame.
func (h *slotHub) SetCellSize(w, ht float32) {
	h.cellPx.Store(&[2]float32{w, ht})
}

func (h *slotHub) cellSizeOrDefault() (float32, float32) {
	if c := h.cellPx.Load(); c != nil && c[0] > 0 && c[1] > 0 {
		return c[0], c[1]
	}
	return 8, 16
}

func (h *slotHub) acceptLoop() {
	for {
		conn, err := h.ln.Accept()
		if err != nil {
			return // listener closed at window exit
		}
		go h.serve(conn)
	}
}

func (h *slotHub) allocate(cols, rows int) (*slot, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id := 0; id < screen.MaxSlots; id++ {
		if _, taken := h.slots[id]; !taken {
			s := &slot{id: id, cols: cols, rows: rows}
			h.slots[id] = s
			return s, nil
		}
	}
	return nil, fmt.Errorf("all %d slots are in use", screen.MaxSlots)
}

// serve runs one pane process's whole conversation: the Open handshake,
// then a read loop relaying its keys and focus to the compositor until
// it closes or the app exits.
func (h *slotHub) serve(conn net.Conn) {
	r := openproto.NewReader(conn)
	msg, err := openproto.ReadMessage(r)
	if err != nil {
		log.Printf("slot hub: read Open: %v", err)
		conn.Close()
		return
	}
	open, ok := msg.(openproto.Open)
	if !ok || len(open.Argv) == 0 {
		h.reject(conn, fmt.Sprintf("expected Open with a command, got %T", msg))
		return
	}
	s, err := h.allocate(open.Cols, open.Rows)
	if err != nil {
		h.reject(conn, err.Error())
		return
	}
	s.client = conn
	if err := h.start(s, open); err != nil {
		log.Printf("slot hub: start %v: %v", open.Argv, err)
		s.close(h)
		h.reject(conn, err.Error())
		return
	}
	if err := s.writeClient(openproto.OpenAck{SlotID: s.id}); err != nil {
		log.Printf("slot hub: send OpenAck: %v", err)
		s.close(h)
		return
	}
	go s.readCompositor(h)
	s.readClient(h, r)
}

func (h *slotHub) reject(conn net.Conn, reason string) {
	if err := openproto.WriteMessage(conn, openproto.OpenReject{Reason: reason}); err != nil {
		log.Printf("slot hub: send OpenReject: %v", err)
	}
	conn.Close()
}

// start launches the app under a fresh tubeless-wm sized to the pane.
func (h *slotHub) start(s *slot, open openproto.Open) error {
	cw, ch := h.cellSizeOrDefault()
	w := max(1, open.Cols) * int(math.Round(float64(cw)))
	ht := max(1, open.Rows) * int(math.Round(float64(ch)))
	wm, theirs, err := wmproto.NewSocketpair()
	if err != nil {
		return err
	}
	args := []string{"--fd", "3", "--size", fmt.Sprintf("%dx%d", w, ht), "--"}
	cmd := exec.Command(h.wmPath, append(args, open.Argv...)...)
	cmd.ExtraFiles = []*os.File{theirs}
	cmd.Env = open.Env
	cmd.Dir = open.Cwd
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	procgroup.Setup(cmd)
	err = cmd.Start()
	theirs.Close()
	if err != nil {
		wm.Close()
		return fmt.Errorf("start %s: %w", wmBinaryName, err)
	}
	s.wm = wm
	s.proc = procgroup.Watch(cmd)
	s.sentW, s.sentH = w, ht
	return nil
}

func (s *slot) writeClient(msg any) error {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	return openproto.WriteMessage(s.client, msg)
}

// readClient relays the pane process's keys and focus to the compositor
// until the pane goes away.
func (s *slot) readClient(h *slotHub, r *bufio.Reader) {
	defer s.close(h)
	for {
		msg, err := openproto.ReadMessage(r)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				log.Printf("slot %d: read from pane process: %v", s.id, err)
			}
			return
		}
		switch m := msg.(type) {
		case openproto.Key:
			s.sendWM(wmproto.Key{Keysym: m.Keysym, Pressed: m.Pressed})
		case openproto.Focus:
			s.sendWM(wmproto.Focus{Focused: m.Focused})
		case openproto.Resize:
			s.mu.Lock()
			s.cols, s.rows = m.Cols, m.Rows
			s.mu.Unlock()
		case openproto.Close:
			return
		default:
			log.Printf("slot %d: unexpected %T from pane process", s.id, msg)
		}
	}
}

// readCompositor maps each new shared frame buffer and records damage
// for the render thread to upload; it never touches GL itself.
func (s *slot) readCompositor(h *slotHub) {
	for {
		msg, err := s.wm.Recv()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				log.Printf("slot %d: compositor: %v", s.id, err)
			}
			s.close(h)
			return
		}
		switch m := msg.(type) {
		case wmproto.Buffer:
			s.adoptBuffer(m)
		case wmproto.Frame:
			s.mu.Lock()
			s.damage.add(m.X, m.Y, m.W, m.H)
			s.mu.Unlock()
		case wmproto.Exited:
			if err := s.writeClient(openproto.Exited{Code: m.Code}); err != nil {
				log.Printf("slot %d: report exit: %v", s.id, err)
			}
			s.close(h)
			return
		}
	}
}

func (s *slot) adoptBuffer(b wmproto.Buffer) {
	size := b.Stride * b.H
	mapped, err := syscall.Mmap(b.FD, 0, size, syscall.PROT_READ, syscall.MAP_SHARED)
	if closeErr := syscall.Close(b.FD); closeErr != nil {
		log.Printf("slot %d: close buffer fd: %v", s.id, closeErr)
	}
	if err != nil {
		log.Printf("slot %d: map frame buffer: %v", s.id, err)
		return
	}
	s.mu.Lock()
	old := s.frame
	s.frame, s.fw, s.fh, s.stride = mapped, b.W, b.H, b.Stride
	s.gen++
	s.damage = damageRect{0, 0, b.W, b.H, true}
	s.mu.Unlock()
	if old != nil {
		if err := syscall.Munmap(old); err != nil {
			log.Printf("slot %d: unmap old frame buffer: %v", s.id, err)
		}
	}
}

func (s *slot) sendWM(msg any) {
	if s.wm == nil {
		return
	}
	if err := s.wm.Send(msg); err != nil {
		log.Printf("slot %d: %v", s.id, err)
	}
}

// close tears the slot down exactly once: stop the app (its whole
// process group, compositor included), drop the connections, and queue
// the texture for release.
func (s *slot) close(h *slotHub) {
	s.closeOnce.Do(func() {
		if s.client != nil {
			s.client.Close()
		}
		if s.proc != nil {
			s.proc.Close(slotCloseGrace)
		}
		if s.wm != nil {
			s.wm.Close()
		}
		s.mu.Lock()
		frame := s.frame
		s.frame = nil
		s.mu.Unlock()
		if frame != nil {
			if err := syscall.Munmap(frame); err != nil {
				log.Printf("slot %d: unmap frame buffer: %v", s.id, err)
			}
		}
		h.mu.Lock()
		delete(h.slots, s.id)
		h.released = append(h.released, s.id)
		h.mu.Unlock()
	})
}

// --- render-thread side ---

// slotLayout is what scan learned about where slots show on the grid.
type slotLayout struct {
	spans []render.SlotSpan
	owner []int16 // slot id per cell, -1 when the cell is not a slot's
	cols  int
}

func (l *slotLayout) ownerAt(col, row int) int {
	i := row*l.cols + col
	if col < 0 || col >= l.cols || row < 0 || i >= len(l.owner) {
		return -1
	}
	return int(l.owner[i])
}

// Frame runs once per render frame on the GL thread: it frees finished
// slots' textures, uploads whatever the compositors changed, and (when
// the terminal screen changed) rescans the grid for slot-marker cells.
// It reports whether the scene needs a redraw.
func (h *slotHub) Frame(r *render.Renderer, scr *screen.Screen, scrollLine int, cs *cellSize, rescan bool) bool {
	if h == nil {
		return false
	}
	h.SetCellSize(cs.w, cs.h)
	h.mu.Lock()
	released := h.released
	h.released = nil
	live := make([]*slot, 0, len(h.slots))
	for _, s := range h.slots {
		live = append(live, s)
	}
	h.mu.Unlock()

	dirty := len(released) > 0
	for _, id := range released {
		r.DropSlot(id)
	}
	for _, s := range live {
		dirty = s.upload(r) || dirty
		dirty = s.syncSize(cs) || dirty
	}
	if rescan || dirty {
		h.layout = h.scan(scr, scrollLine, live)
		r.SetSlotSpans(h.layout.spans)
		dirty = true
	}
	return dirty
}

// upload pushes the compositor's damaged rect into the slot's texture.
func (s *slot) upload(r *render.Renderer) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.frame == nil {
		return false
	}
	tex := r.SlotTexture(s.id)
	if s.gen != s.uploadedGen {
		tex.Resize(s.fw, s.fh)
		s.uploadedGen = s.gen
	}
	d := s.damage
	if !d.set {
		return false
	}
	s.damage = damageRect{}
	x0, y0 := max(d.x0, 0), max(d.y0, 0)
	x1, y1 := min(d.x1, s.fw), min(d.y1, s.fh)
	if x1 <= x0 || y1 <= y0 {
		return false
	}
	tex.UpdateRect(x0, y0, x1-x0, y1-y0, s.frame, s.stride/4)
	return true
}

// syncSize asks the compositor to match the pane's pixel size whenever
// the pane or the cell size changes.
func (s *slot) syncSize(cs *cellSize) bool {
	s.mu.Lock()
	cols, rows := s.cols, s.rows
	s.mu.Unlock()
	w := max(1, cols) * int(math.Round(float64(cs.w)))
	h := max(1, rows) * int(math.Round(float64(cs.h)))
	if w == s.sentW && h == s.sentH {
		return false
	}
	s.sentW, s.sentH = w, h
	s.sendWM(wmproto.Resize{W: w, H: h})
	return false
}

// scan finds every slot-marker cell on the visible grid and works out,
// per slot, which cells show it and which part of the app's frame each
// shows. A slot's rect is the bounding box of its marker cells whenever
// that matches the pane's reported size; while a tmux popup covers part
// of the pane the box shrinks, so the last full-size position is kept
// and the covered cells are simply not painted.
func (h *slotHub) scan(scr *screen.Screen, scrollLine int, live []*slot) *slotLayout {
	grid := scr.VisibleWindow(scrollLine)
	l := &slotLayout{cols: scr.Cols, owner: make([]int16, scr.Cols*scr.Rows)}
	for i := range l.owner {
		l.owner[i] = -1
	}
	type bbox struct{ x0, y0, x1, y1 int }
	boxes := make(map[int]*bbox)
	for y, row := range grid {
		for x, cell := range row {
			id, ok := cell.SlotID()
			if !ok {
				continue
			}
			l.owner[y*scr.Cols+x] = int16(id)
			b := boxes[id]
			if b == nil {
				boxes[id] = &bbox{x, y, x, y}
				continue
			}
			b.x0, b.y0 = min(b.x0, x), min(b.y0, y)
			b.x1, b.y1 = max(b.x1, x), max(b.y1, y)
		}
	}
	for _, s := range live {
		b := boxes[s.id]
		if b == nil {
			continue
		}
		s.mu.Lock()
		cols, rows := max(1, s.cols), max(1, s.rows)
		s.mu.Unlock()
		full := b.x1-b.x0+1 == cols && b.y1-b.y0+1 == rows
		if full || !s.haveAnchor {
			s.anchorCol, s.anchorRow, s.haveAnchor = b.x0, b.y0, true
		}
		l.spans = append(l.spans, slotSpans(s, l, scr, cols, rows)...)
	}
	return l
}

// slotSpans turns one slot's marker cells into horizontal runs, each
// with the texture rect its cells show.
func slotSpans(s *slot, l *slotLayout, scr *screen.Screen, cols, rows int) []render.SlotSpan {
	var out []render.SlotSpan
	for y := 0; y < scr.Rows; y++ {
		ry := y - s.anchorRow
		if ry < 0 || ry >= rows {
			continue
		}
		x := 0
		for x < scr.Cols {
			if l.ownerAt(x, y) != s.id {
				x++
				continue
			}
			start := x
			for x < scr.Cols && l.ownerAt(x, y) == s.id {
				x++
			}
			out = append(out, runSpan(s, start, x, y, ry, cols, rows)...)
		}
	}
	return out
}

// runSpan clips a run of marker cells to the pane's rect.
func runSpan(s *slot, x0, x1, y, ry, cols, rows int) []render.SlotSpan {
	rx0, rx1 := max(x0-s.anchorCol, 0), min(x1-s.anchorCol, cols)
	if rx1 <= rx0 {
		return nil
	}
	return []render.SlotSpan{{
		ID:    s.id,
		Col:   s.anchorCol + rx0,
		Row:   y,
		Cells: rx1 - rx0,
		U0:    float32(rx0) / float32(cols),
		U1:    float32(rx1) / float32(cols),
		V0:    float32(ry) / float32(rows),
		V1:    float32(ry+1) / float32(rows),
	}}
}

// HitTest reports which slot shows at the window pixel position (px, py
// relative to the grid's top-left), and the pixel offset inside that
// slot's frame.
func (h *slotHub) HitTest(gx, gy float32, cs *cellSize) (*slot, int, int, bool) {
	if h.layout == nil || cs.w <= 0 || cs.h <= 0 {
		return nil, 0, 0, false
	}
	col, row := int(gx/cs.w), int(gy/cs.h)
	id := h.layout.ownerAt(col, row)
	if id < 0 {
		return nil, 0, 0, false
	}
	h.mu.Lock()
	s := h.slots[id]
	h.mu.Unlock()
	if s == nil || !s.haveAnchor {
		return nil, 0, 0, false
	}
	x := int(gx - float32(s.anchorCol)*cs.w)
	y := int(gy - float32(s.anchorRow)*cs.h)
	return s, max(x, 0), max(y, 0), true
}
