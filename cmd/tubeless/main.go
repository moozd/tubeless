package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/go-gl/glfw/v3.4/glfw"

	"github.com/moozd/tubeless/pkg/font"
	"github.com/moozd/tubeless/pkg/ptyio"
	"github.com/moozd/tubeless/pkg/render"
	"github.com/moozd/tubeless/pkg/screen"
	"github.com/moozd/tubeless/pkg/theme"
	"github.com/moozd/tubeless/pkg/vtparse"
)

// GLFW/OpenGL require every GL call to run on the exact OS thread the
// context was made current on. Without this, Go's scheduler is free to
// migrate main()'s goroutine to a different OS thread at any preemption
// point, which silently breaks the current context — GL calls then hit a
// thread with nothing current, and driver behavior in that state is
// undefined (commonly: status queries read back zeroed rather than
// erroring), which reads as flaky, unexplainable rendering glitches.
func init() {
	runtime.LockOSThread()
}

const (
	cols, rows         = 90, 30
	defaultPixelHeight = 20
	// 1.0 (no-op): glyph blending is now genuinely gamma-correct via an
	// sRGB framebuffer (see pkg/render/window.go's GL_FRAMEBUFFER_SRGB
	// and fbo.go's newSRGBFBO), so this coverage-curve approximation is
	// no longer needed — stacking both would double-correct.
	fontGamma         = 1.0
	windowScale       = 1
	cursorBlinkPeriod = 530 * time.Millisecond

	// atlasScale rasterizes the font atlas this many times larger than the
	// on-screen cell size. The extra detail only pays off because
	// uploadAtlas mipmaps the atlas texture — the GPU's trilinear
	// minification down to display size is what actually smooths glyph
	// edges, not the source rasterization by itself. Kept lower than the
	// quality-only ceiling (8x) now that EnumerateRunes pulls in a Nerd
	// Font's full icon set (thousands of codepoints, not ~350) — 4x still
	// looked good in testing and keeps the atlas texture comfortably
	// under typical GL_MAX_TEXTURE_SIZE limits.
	atlasScale = 4
)

type resizeReq struct{ cols, rows int }

func main() {
	themeName := flag.String("theme", "green", "amber | green")
	shell := flag.String("shell", "", "program to run instead of $SHELL")
	fontPath := flag.String("font", "", "path to a TTF/OTF font (default: bundled IBM 3270)")
	fontSize := flag.Int("font-size", defaultPixelHeight, "logical font size in pixels")
	flag.Parse()

	th, ok := theme.ByName(*themeName)
	if !ok {
		log.Fatalf("unknown theme %q (want amber or green)", *themeName)
	}

	fontBytes := loadFontBytes(*fontPath)
	runes, err := font.EnumerateRunes(fontBytes)
	if err != nil {
		log.Fatalf("enumerate font glyphs: %v", err)
	}
	atlas, err := font.Build(fontBytes, runes, *fontSize*atlasScale, fontGamma)
	if err != nil {
		log.Fatalf("build font atlas: %v", err)
	}

	sess := startShell(*shell)
	defer sess.Close()

	var shared atomic.Pointer[screen.Screen]
	resizeCh := make(chan resizeReq, 1)
	shared.Store(screen.New(cols, rows))
	go ptyCoordinator(sess, &shared, resizeCh)

	winW := cols * (atlas.CellWidth / atlasScale) * windowScale
	winH := rows * (atlas.CellHeight / atlasScale) * windowScale
	win, err := render.NewWindow(fmt.Sprintf("tubeless (%s)", th.Name), winW, winH)
	if err != nil {
		log.Fatalf("open window: %v", err)
	}
	defer win.Destroy()

	renderer, err := render.New(atlas, cols, rows)
	if err != nil {
		log.Fatalf("init renderer: %v", err)
	}

	// On Wayland, content scale arrives asynchronously — the compositor's
	// wp_fractional_scale_v1 "preferred_scale" event is a genuine round
	// trip over the socket, and how long that takes is unpredictable
	// (observed anywhere from ~130ms to several seconds under load), so
	// no fixed startup wait is reliable. cs is shared (mutated only from
	// this locked OS thread, alongside every other GLFW/GL call, so no
	// synchronization is needed) between the short best-effort wait
	// below, the content-scale callback that corrects it whenever the
	// real value does arrive — even well after startup, or if the window
	// moves to a differently-scaled monitor later — and the render loop.
	dpiX, dpiY := win.GetContentScale()
	for i := 0; i < 5 && dpiX == 1 && dpiY == 1; i++ {
		glfw.WaitEventsTimeout(0.05)
		dpiX, dpiY = win.GetContentScale()
	}
	cs := &cellSize{
		w: float32(atlas.CellWidth) / atlasScale * dpiX,
		h: float32(atlas.CellHeight) / atlasScale * dpiY,
	}
	wireResize(win, resizeCh, cs)
	win.SetContentScaleCallback(func(_ *glfw.Window, x, y float32) {
		cs.w = float32(atlas.CellWidth) / atlasScale * x
		cs.h = float32(atlas.CellHeight) / atlasScale * y
		pushResize(win, resizeCh, cs)
	})

	wireInput(win, sess)
	runLoop(win, renderer, &shared, th, cs)
}

// cellSize is the physical-pixel size of one cell — fixed regardless of
// window size (resizing reflows the grid instead of stretching glyphs),
// but not fixed for the process lifetime: it depends on display content
// scale, which can change (see SetContentScaleCallback above).
type cellSize struct{ w, h float32 }

func loadFontBytes(path string) []byte {
	if path == "" {
		return font.DefaultFontBytes()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("read font %s: %v", path, err)
	}
	return data
}

// ptyCoordinator is the sole mutator of the terminal's working Screen. It
// reacts to two independent event sources — PTY output and resize
// requests — without either blocking the other: a resize while the shell
// is idle (no PTY output pending) must still take effect immediately, not
// wait for the next byte to arrive on a blocking Read.
func ptyCoordinator(sess *ptyio.Session, shared *atomic.Pointer[screen.Screen], resizeCh <-chan resizeReq) {
	work := screen.New(cols, rows)
	handler := screen.NewHandler(work)
	parser := vtparse.New(handler)

	readCh := make(chan []byte)
	go pumpPTYOutput(sess, readCh)

	for {
		select {
		case chunk, ok := <-readCh:
			if !ok {
				return
			}
			parser.Write(chunk)
			shared.Store(work.Clone())
		case req := <-resizeCh:
			if req.cols == work.Cols && req.rows == work.Rows {
				continue
			}
			work.Resize(req.cols, req.rows)
			if err := sess.Resize(req.cols, req.rows); err != nil {
				log.Printf("pty resize: %v", err)
			}
			shared.Store(work.Clone())
		}
	}
}

// wireResize reflows the grid (and the PTY's reported size) to fill the
// window at a fixed cell pixel size, instead of stretching glyphs. cs is
// read fresh on every callback (see cellSize) so a content-scale change
// between resizes is picked up automatically, not just at wireResize's
// call time.
func wireResize(win *render.Window, resizeCh chan resizeReq, cs *cellSize) {
	win.SetFramebufferSizeCallback(func(_ *glfw.Window, width, height int) {
		pushResizeSize(resizeCh, cs, width, height)
	})
}

// pushResize recomputes cols/rows for the window's current framebuffer
// size against cs and sends it — used both by wireResize's callback and
// by the content-scale callback, which needs to reflow immediately when
// cs itself changes rather than waiting for an unrelated resize event.
func pushResize(win *render.Window, resizeCh chan resizeReq, cs *cellSize) {
	w, h := win.FramebufferPixelSize()
	pushResizeSize(resizeCh, cs, w, h)
}

// The send is non-blocking and drops a stale pending resize in favor of
// the newest one, so a burst of resize events during a drag never backs
// up.
func pushResizeSize(resizeCh chan resizeReq, cs *cellSize, w, h int) {
	req := resizeReq{cols: max(1, int(float32(w)/cs.w)), rows: max(1, int(float32(h)/cs.h))}
	select {
	case resizeCh <- req:
	default:
		select {
		case <-resizeCh:
		default:
		}
		resizeCh <- req
	}
}

func startShell(shell string) *ptyio.Session {
	name := shell
	if name == "" {
		name = os.Getenv("SHELL")
	}
	if name == "" {
		name = "/bin/sh"
	}
	sess, err := ptyio.Start(name, nil, cols, rows)
	if err != nil {
		log.Fatalf("start shell: %v", err)
	}
	return sess
}

// pumpPTYOutput only ever reads and forwards — it never touches Screen —
// so a slow consumer downstream can't leave the PTY's kernel buffer full,
// and this goroutine never needs to know about resizes or rendering.
func pumpPTYOutput(sess *ptyio.Session, out chan<- []byte) {
	defer close(out)
	buf := make([]byte, 4096)
	for {
		n, err := sess.Master.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			out <- chunk
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("pty read: %v", err)
			}
			return
		}
	}
}

// runLoop only does GPU work when something actually needs to be shown:
// the published Screen changed, the framebuffer was resized, or the
// cursor's blink phase flipped. glfw.WaitEventsTimeout blocks (no CPU/GPU
// cost) until whichever comes first — an input/resize event, or the blink
// period elapsing — so a keypress redraws immediately instead of waiting
// on a fixed render cadence, and an idle shell prompt costs nothing.
func runLoop(win *render.Window, renderer *render.Renderer, shared *atomic.Pointer[screen.Screen], th theme.Theme, cs *cellSize) {
	var lastScr *screen.Screen
	var lastW, lastH int
	blinkOn := true
	lastBlink := time.Now()

	for !win.ShouldClose() {
		glfw.WaitEventsTimeout(cursorBlinkPeriod.Seconds())

		w, h := win.FramebufferPixelSize()
		scr := shared.Load()
		dirty := scr != lastScr || w != lastW || h != lastH

		if scr != lastScr {
			blinkOn, lastBlink = true, time.Now()
		} else if time.Since(lastBlink) >= cursorBlinkPeriod {
			blinkOn, lastBlink = !blinkOn, time.Now()
			dirty = true
		}

		if !dirty {
			continue
		}
		cursorOn := scr.CursorVisible && blinkOn
		renderer.PrepareFrame(scr, th, cs.w, cs.h, cursorOn)
		renderer.RenderFrame(w, h, cs.w, cs.h)
		win.SwapBuffers()
		lastScr, lastW, lastH = scr, w, h
	}
}
