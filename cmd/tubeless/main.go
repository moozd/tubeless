// Command tubeless is a GPU-rendered terminal emulator. It's a normal
// foreground GUI process — it opens a window and doesn't return until
// that window closes — so launching it straight from a shell blocks that
// shell's prompt until you close the window, same as any other GUI app
// invoked without backgrounding. Run it as `tubeless &` (or via a desktop
// entry, which launches detached by construction) if you want the shell
// back immediately.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-gl/glfw/v3.4/glfw"

	"github.com/moozd/tubeless/pkg/config"
	"github.com/moozd/tubeless/pkg/font"
	"github.com/moozd/tubeless/pkg/platform"
	"github.com/moozd/tubeless/pkg/ptyio"
	"github.com/moozd/tubeless/pkg/render"
	"github.com/moozd/tubeless/pkg/screen"
	"github.com/moozd/tubeless/pkg/upgrade"
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
	cols, rows = 90, 30

	// 1.0 (no-op): glyph blending is now genuinely gamma-correct via an
	// sRGB framebuffer (see pkg/render/window.go's GL_FRAMEBUFFER_SRGB
	// and fbo.go's newSRGBFBO), so this coverage-curve approximation is
	// no longer needed — stacking both would double-correct.
	fontGamma   = 1.0
	windowScale = 1
)

type resizeReq struct{ cols, rows int }

// version is baked in at build time via -ldflags "-X main.version=..."
// (see the Makefile's LDFLAGS) — "dev" for a plain `go build` outside it.
var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-version") {
		fmt.Println("tubeless " + version)
		return
	}

	// Must run before any exec.Command below (fc-list via font.SystemFamilies,
	// the pty shell itself) — a macOS GUI process launched from Finder/Dock
	// inherits launchd's minimal PATH, missing whatever a login shell's
	// .zprofile/.zshrc adds (Homebrew's shellenv chief among them).
	platform.FixEnv()

	if len(os.Args) > 1 && os.Args[1] == "config" {
		runConfigTUI()
	}
	if len(os.Args) > 1 && os.Args[1] == "upgrade" {
		runUpgrade()
	}

	// Registered before anything else — critically, before any cgo call
	// (font.Build's FreeType binding is the first one below). Something
	// in FreeType's C-side initialization resets SIGTERM's process-wide
	// disposition back to default if Go's own handler is installed after
	// it: registering late meant Ctrl-C/SIGTERM killed the process
	// outright (skipping sess.Close()/win.Destroy() below) even though
	// signal.Notify looked like it should have caught it — confirmed by
	// bisecting a minimal repro. Registering first, before FreeType ever
	// runs, avoids the clobber entirely; closeRequested lets runLoop exit
	// through its normal cleanup path instead of the process dying
	// mid-signal.
	closeRequested := new(atomic.Bool)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		closeRequested.Store(true)
	}()

	fs := flag.NewFlagSet("tubeless", flag.ExitOnError)
	themeName := fs.String("theme", "", fmt.Sprintf("starting theme preset: %s (config file overrides)", strings.Join(config.PresetNames(), " | ")))
	shell := fs.String("shell", "", "program to run instead of $SHELL")
	fontFamily := fs.String("font", "", "installed font family name (default: bundled FiraCode Nerd)")
	fontSize := fs.Int("font-size", 0, "logical font size in pixels (default: config font.size)")
	fs.Parse(os.Args[1:])

	cfg, cfgPath, err := loadConfig(fs, *themeName, *fontFamily, *fontSize)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	resolve := func() config.Config {
		c, _, err := loadConfig(fs, *themeName, *fontFamily, *fontSize)
		if err != nil {
			log.Printf("reload config: %v", err)
			return cfg
		}
		return c
	}

	maxTextureSize, err := render.ProbeMaxTextureSize()
	if err != nil {
		log.Fatalf("probe GPU texture limit: %v", err)
	}
	// Best-effort guess at the display the window is about to open on —
	// there's no window yet to ask directly (see effectiveAtlasScale;
	// the atlas has to exist before the window can be sized for it). If
	// this turns out wrong (a multi-monitor setup that doesn't open on
	// the primary, or Wayland's real scale arriving late), the window
	// size itself is unaffected — dividing by the same effectiveScale
	// used to build it cancels the DPI factor back out — and the
	// correction right after window creation below fixes the atlas
	// itself before the first frame ever renders.
	initDpiX, _, err := render.PrimaryMonitorContentScale()
	if err != nil {
		log.Fatalf("probe display scale: %v", err)
	}

	faces, effectiveScale, err := buildFacesFor(cfg, initDpiX, maxTextureSize)
	if err != nil {
		log.Fatalf("%v", err)
	}

	sess := startShell(*shell)
	defer sess.Close()
	// log.Fatalf calls os.Exit internally, which would skip the deferred
	// sess.Close() above and leak the already-spawned shell — every fatal
	// error from here on must close it explicitly first.
	fatal := func(format string, args ...any) {
		log.Printf(format, args...)
		sess.Close()
		os.Exit(1)
	}

	var shared atomic.Pointer[screen.Screen]
	resizeCh := make(chan resizeReq, 1)
	shared.Store(screen.New(cols, rows))
	go ptyCoordinator(sess, &shared, resizeCh, cfg.Scrollback.Lines, closeRequested)

	winW := cols * (faces.Regular.CellWidth / effectiveScale) * windowScale
	winH := rows * (faces.Regular.CellHeight / effectiveScale) * windowScale
	win, err := render.NewWindow(fmt.Sprintf("tubeless (%s)", cfg.Theme), winW, winH)
	if err != nil {
		fatal("open window: %v", err)
	}
	defer win.Destroy()

	renderer, err := render.New(faces, cols, rows)
	if err != nil {
		fatal("init renderer: %v", err)
	}

	// On Wayland, content scale arrives asynchronously — the compositor's
	// wp_fractional_scale_v1 "preferred_scale" event is a genuine round
	// trip over the socket, and how long that takes is unpredictable
	// (observed anywhere from ~130ms to several seconds under load), so
	// no fixed startup wait is reliable. This is a best-effort check
	// only — runLoop's own per-frame re-check (see there) is what
	// actually guarantees the atlas eventually matches reality, on
	// platforms (observed on macOS) where GLFW doesn't deliver the real
	// scale via GetContentScale until well after the window opens,
	// sometimes not until a later, unrelated event (a real resize,
	// entering fullscreen) prods it. CurrentMonitorContentScale (see its
	// own doc comment) sidesteps that staleness by asking the monitor
	// directly instead of the window.
	dpiX, dpiY := win.CurrentMonitorContentScale()
	for i := 0; i < 5 && dpiX == 1 && dpiY == 1; i++ {
		glfw.WaitEventsTimeout(0.05)
		dpiX, dpiY = win.CurrentMonitorContentScale()
	}
	if dpiX != initDpiX {
		// The pre-window guess didn't match this display — rebuild now
		// so the very first rendered frame is already at the right
		// quality, rather than looking soft for a frame or two until
		// runLoop's own check catches up.
		if newFaces, newScale, err := buildFacesFor(cfg, dpiX, render.MaxTextureSize()); err != nil {
			log.Printf("rebuild atlas for display scale %.2f: %v", dpiX, err)
		} else if newRenderer, err := render.New(newFaces, cols, rows); err != nil {
			log.Printf("rebuild renderer for display scale %.2f: %v", dpiX, err)
		} else {
			faces, effectiveScale, renderer = newFaces, newScale, newRenderer
		}
	}
	cs := &cellSize{
		w:    float32(faces.Regular.CellWidth) / float32(effectiveScale) * dpiX,
		h:    float32(faces.Regular.CellHeight) / float32(effectiveScale) * dpiY,
		dpiX: dpiX,
		dpiY: dpiY,
	}
	wireResize(win, resizeCh, cs)

	sel := &render.Selection{}
	wireInput(win, sess, &shared, sel)
	scroll := &scrollState{}
	wireMouse(win, sess, &shared, scroll, cs, sel)

	// focused tracks real window focus, read/written only from this
	// locked OS thread (see runLoop's visibility gate) — no
	// synchronization needed, same as cs above.
	focused := win.GetAttrib(glfw.Focused) == glfw.True
	win.SetFocusCallback(func(_ *glfw.Window, isFocused bool) {
		focused = isFocused
	})

	runLoop(win, renderer, &shared, cfg, cs, cfgPath, resolve, closeRequested, scroll, sel, resizeCh, &focused)
}

// runUpgrade runs `tubeless upgrade`: checks GitHub for a release newer
// than this build's version, downloads this platform/arch's asset,
// replaces the installed binaries/app bundle in place, and relaunches —
// see pkg/upgrade for the actual mechanics. Like runConfigTUI, this is a
// plain terminal command and never opens a window.
func runUpgrade() {
	if err := upgrade.Run(version, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "upgrade: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// runConfigTUI runs `tubeless config`: cmd/tubeless-config is a plain
// ANSI/termios program (no GLFW, no window of its own — see its own
// doc comment), so this hands off to it directly in the terminal the user
// already typed the command into, inheriting stdio, and exits with
// whatever it exits with. It deliberately never opens a tubeless window —
// that would launch a whole second terminal emulator instance just to
// edit a config file.
func runConfigTUI() {
	bin, err := locateConfigBinary()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	cmd := exec.Command(bin, os.Args[2:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	// This process already ran platform.FixEnv (see main's top) — don't
	// have the child repeat ensureCLIOnPath's exact same PATH-symlink
	// writes (and, on failure, the exact same warning) a second time.
	cmd.Env = append(os.Environ(), platform.SkipPathHealEnv+"=1")
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "run %s: %v\n", bin, err)
		os.Exit(1)
	}
	os.Exit(0)
}

// locateConfigBinary finds the tubeless-config binary `tubeless config`
// should hand off to: first next to this executable (the common case —
// `make build` and `make install` both put the two binaries in the same
// directory), falling back to $PATH for any other layout.
func locateConfigBinary() (string, error) {
	if self, err := os.Executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(self), "tubeless-config")
		if info, err := os.Stat(sibling); err == nil && !info.IsDir() {
			return sibling, nil
		}
	}
	if p, err := exec.LookPath("tubeless-config"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("tubeless-config not found next to %s or on PATH — build it with `make config-test`", os.Args[0])
}

// loadConfig resolves the effective settings for a terminal run: the
// starting preset (--theme, else the file's theme key, else rosepine) is
// layered with the config file and then the explicitly-set flags.
func loadConfig(fs *flag.FlagSet, flagTheme, flagFamily string, flagSize int) (config.Config, string, error) {
	path, err := config.DefaultPath()
	if err != nil {
		return config.Preset("rosepine"), "", err
	}
	cfg, err := config.Load(path, flagTheme)
	if err != nil {
		return cfg, path, err
	}
	explicit := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	if explicit["font"] {
		cfg.Font.Family = flagFamily
	}
	if explicit["font-size"] {
		cfg.Font.Size = flagSize
	}
	return cfg, path, nil
}

// cellSize is the physical-pixel size of one cell — fixed regardless of
// window size (resizing reflows the grid instead of stretching glyphs),
// but not fixed for the process lifetime: it depends on display content
// scale, which can change (see runLoop's per-frame content-scale check).
// dpiX/dpiY remember the scale the current atlas was actually built for.
type cellSize struct{ w, h, dpiX, dpiY float32 }

// effectiveAtlasScale turns cfg.Atlas.Scale — the supersampling-vs-mip
// depth the user actually wants, calibrated by eye on whatever display
// they're looking at — into the raw raster multiplier BuildFaces needs,
// by scaling it up with the current display's own DPI factor. Without
// this, the same cfg.Atlas.Scale means a much deeper (and, at least on
// some GPU drivers, visibly blurrier) mipmap minification on a
// standard-DPI display than on a Retina one, since the on-screen cell
// size the atlas gets minified down to is itself proportional to DPI —
// moving the raster resolution up by the same factor keeps the ratio
// between the two constant, so one number looks the same everywhere.
// dpi <= 0 is treated as 1 (unknown/unreported scale).
func effectiveAtlasScale(scale int, dpi float32) int {
	if dpi <= 0 {
		dpi = 1
	}
	return max(1, int(math.Round(float64(scale)*float64(dpi))))
}

// buildFacesFor builds cfg's glyph atlas at the raster resolution
// effectiveAtlasScale computes for dpi, returning that effective scale
// alongside the faces since callers need it again to convert the
// atlas's raster cell size back to physical pixels (see cellSize).
// GL-independent — safe to call before any window exists.
//
// A cfg.Atlas.Scale that's too high for maxTextureSize at the current
// DPI is stepped down (logging each attempt) until one fits, rather
// than failing outright: DPI is now part of the effective raster
// resolution (see effectiveAtlasScale), so a value that was fine on one
// display — or under the old, DPI-blind formula this replaced — can
// exceed the limit on a higher-DPI one without the user ever having
// touched their config. Crashing the whole app on launch over a stale
// number is worse than a shell-visible warning and a slightly softer
// atlas.
func buildFacesFor(cfg config.Config, dpi float32, maxTextureSize int) (*font.Faces, int, error) {
	scale := cfg.Atlas.Scale
	for {
		effectiveScale := effectiveAtlasScale(scale, dpi)
		faceBytes := loadFontFaces(cfg.Font.Family)
		faces, err := font.BuildFaces(faceBytes, cfg.Font.Size*effectiveScale, cfg.Atlas.Gamma, effectiveScale, cfg.Font.LineHeight, maxTextureSize)
		if err == nil {
			if scale != cfg.Atlas.Scale {
				log.Printf("atlas.scale %d was too high for this display/GPU; using %d instead — lower it in your config to stop seeing this", cfg.Atlas.Scale, scale)
			}
			return faces, effectiveScale, nil
		}
		if scale <= 1 {
			return nil, 0, fmt.Errorf("build font atlas: %w", err)
		}
		scale--
	}
}

// loadFontBytes resolves cfg.Font.Family to a font file via fontconfig
// (see font.ResolveFamily) and reads it. An empty family, or any failure
// to resolve/read one, falls back to the bundled font rather than failing
// startup — a bad family name (a typo, fontconfig not installed) should
// read as "wrong font", never a crash.
func loadFontBytes(family string) []byte {
	if family == "" {
		return font.DefaultFontBytes()
	}
	path, err := font.ResolveFamily(family)
	if err != nil {
		log.Printf("resolve font family %q: %v — using bundled font", family, err)
		return font.DefaultFontBytes()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("read font %s: %v — using bundled font", path, err)
		return font.DefaultFontBytes()
	}
	return data
}

// loadFontFaces resolves all four style variants for family. Regular is
// loadFontBytes unchanged. Bold always ends up with something real: a
// system family's own Bold cut if fontconfig genuinely has one (see
// font.ResolveStyle), otherwise the bundled Bold cut — never a
// brightness-only fake. Italic/BoldItalic are left nil unless a system
// family genuinely has that cut; CellPass falls back to a synthetic
// slant of Regular/Bold in that case rather than these holding a
// fontconfig substitute that isn't actually italic.
func loadFontFaces(family string) font.FaceBytes {
	readStyle := func(style string) []byte {
		if family == "" {
			return nil
		}
		path, ok := font.ResolveStyle(family, style)
		if !ok {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("read font %s: %v", path, err)
			return nil
		}
		return data
	}
	bold := readStyle("Bold")
	if bold == nil {
		bold = font.DefaultBoldFontBytes()
	}
	return font.FaceBytes{
		Regular:    loadFontBytes(family),
		Bold:       bold,
		Italic:     readStyle("Italic"),
		BoldItalic: readStyle("Bold Italic"),
	}
}

// ptyCoordinator is the sole mutator of the terminal's working Screen. It
// reacts to two independent event sources — PTY output and resize
// requests — without either blocking the other: a resize while the shell
// is idle (no PTY output pending) must still take effect immediately, not
// wait for the next byte to arrive on a blocking Read.
func ptyCoordinator(sess *ptyio.Session, shared *atomic.Pointer[screen.Screen], resizeCh <-chan resizeReq, scrollbackLines int, closeRequested *atomic.Bool) {
	work := screen.New(cols, rows)
	work.SetScrollbackCap(scrollbackLines)
	handler := screen.NewHandler(work)
	parser := vtparse.New(handler)

	readCh := make(chan []byte)
	go pumpPTYOutput(sess, readCh)

	for {
		select {
		case chunk, ok := <-readCh:
			if !ok {
				// The shell exited (pumpPTYOutput's read hit EOF and closed
				// readCh) — without this, runLoop would never learn the
				// shell is gone and the window would sit open forever with
				// the shell left unreaped as a zombie.
				closeRequested.Store(true)
				return
			}
			parser.Write(chunk)
			// A single redraw an app writes in one syscall (a full-screen
			// clear-and-redraw, a colored highlight bar) can still arrive
			// here split across multiple 4KB reads (see pumpPTYOutput), or
			// as several small writes a few hundred microseconds apart (a
			// pager or editor repainting one line at a time, tmux relaying
			// a pane's redraw through its own pty) — publishing after every
			// one of those doesn't just risk a frame landing mid-redraw, it
			// publishes each intermediate line as its own complete Screen,
			// so a scroll that only reads as one once it's finished instead
			// renders as a rapid sequence of one-line jumps, and it means
			// work.PendingScrolls() below would only ever hold one small
			// piece of what's really a single continuous scroll (see
			// pkg/render's ApplyScrollEvents, which nets same-region shifts
			// but can only net what it's actually given in one batch).
			// Draining whatever's already queued, then waiting a short
			// quiet window for more before publishing, coalesces a burst
			// back into the single complete frame it actually is.
			drainPending(readCh, parser)
			shared.Store(work.Clone())
			work.ClearPendingScrolls()
		case req := <-resizeCh:
			if req.cols == work.Cols && req.rows == work.Rows {
				continue
			}
			work.Resize(req.cols, req.rows)
			if err := sess.Resize(req.cols, req.rows); err != nil {
				log.Printf("pty resize: %v", err)
			}
			shared.Store(work.Clone())
			work.ClearPendingScrolls()
		}
	}
}

// pubCoalesceQuiet is how long drainPending waits after the last chunk for
// another one before giving up and letting ptyCoordinator publish — long
// enough to catch the next line of a multi-line redraw arriving a few
// hundred microseconds later, short enough to stay well under a frame
// (imperceptible added latency for an isolated keystroke's echo).
// pubCoalesceMax bounds the total time a single publish can be held back
// by a continuous stream (heavy scrollback-filling output, a fast
// redraw): once a burst has been running this long, publish what's been
// parsed so far rather than starving the screen of any update at all.
const (
	pubCoalesceQuiet = 3 * time.Millisecond
	pubCoalesceMax   = 12 * time.Millisecond
)

// drainPending feeds parser every chunk already sitting in readCh, then
// keeps waiting up to pubCoalesceQuiet for one more (resetting that
// window each time one arrives) so a burst of small, closely-spaced
// writes — see ptyCoordinator's case above for why that matters — gets
// parsed as a whole before publishing, capped overall by pubCoalesceMax
// so a continuous stream still publishes regularly instead of starving.
func drainPending(readCh <-chan []byte, parser *vtparse.Parser) {
	deadline := time.Now().Add(pubCoalesceMax)
	timer := time.NewTimer(pubCoalesceQuiet)
	defer timer.Stop()
	for {
		select {
		case chunk, ok := <-readCh:
			if !ok {
				return
			}
			parser.Write(chunk)
			if time.Now().After(deadline) {
				return
			}
			if !timer.Stop() {
				<-timer.C
			}
			timer.Reset(pubCoalesceQuiet)
		case <-timer.C:
			return
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
	// -l (login shell) only for the $SHELL auto-detect path: it's what
	// every other terminal emulator does by default (Terminal.app, iTerm,
	// kitty, Alacritty) so .zprofile/.bash_profile PATH setup actually
	// runs, but an explicit --shell override (e.g. tektest for run-green)
	// gets exactly the program named, no injected flag it may not accept.
	args := []string{"-l"}
	if name == "" {
		name = os.Getenv("SHELL")
	} else {
		args = nil
	}
	if name == "" {
		name = "/bin/sh"
	}
	sess, err := ptyio.Start(name, args, cols, rows)
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

// newRendererFor rebuilds the glyph atlas + renderer for cfg at the
// window's current display scale and updates cs with the resulting
// physical cell size. Used by runLoop both when the config file's
// font/atlas settings change at runtime, and when the display's content
// scale itself changes (a monitor switch, or GLFW's delayed real-scale
// delivery — see runLoop's per-frame check) — either way, the atlas
// needs to be rebuilt at the new effective scale, not just have cs's
// cell size adjusted proportionally, or the raster resolution keeps
// matching whatever display was current the last time it was built.
func newRendererFor(win *render.Window, cfg config.Config, cs *cellSize) (*render.Renderer, error) {
	dx, dy := win.CurrentMonitorContentScale()
	if dx == 0 {
		dx, dy = 1, 1
	}
	faces, effectiveScale, err := buildFacesFor(cfg, dx, render.MaxTextureSize())
	if err != nil {
		return nil, err
	}
	r, err := render.New(faces, cols, rows)
	if err != nil {
		return nil, fmt.Errorf("init renderer: %w", err)
	}
	cs.dpiX, cs.dpiY = dx, dy
	cs.w = float32(faces.Regular.CellWidth) / float32(effectiveScale) * dx
	cs.h = float32(faces.Regular.CellHeight) / float32(effectiveScale) * dy
	return r, nil
}

// cfgWatch polls the config file's mtime so live edits (made by the
// in-terminal config TUI, or by hand) are picked up without tight-looping
// os.Stat.
type cfgWatch struct {
	path string
	mod  time.Time
	ok   bool
	next time.Time
}

func (w *cfgWatch) changed(now time.Time) bool {
	if now.Before(w.next) {
		return false
	}
	w.next = now.Add(200 * time.Millisecond)
	st, err := os.Stat(w.path)
	if err != nil {
		changed := w.ok
		w.ok = false
		return changed
	}
	if !w.ok {
		w.ok, w.mod = true, st.ModTime()
		return true
	}
	if !st.ModTime().Equal(w.mod) {
		w.mod = st.ModTime()
		return true
	}
	return false
}

// runLoop drives a continuous, vsync-paced render so the cursor can
// animate in real time. The scene (cell buffers + shape blur) is
// dirty-gated — it only rebuilds when the published Screen changed or the
// framebuffer resized — while the cheap fullscreen passes (cursor glow,
// inset composite) run every frame so the cursor glides even with the
// shell idle. SwapBuffers blocks on vsync (see window.go's SwapInterval),
// so an idle visible window costs the fullscreen passes at display
// refresh, not a busy spin.
//
// cfgPath + resolve let the config TUI's edits apply live: when the file
// changes, non-font settings are re-applied on the next scene rebuild, and
// font/atlas changes rebuild the renderer (which reflows the grid via cs).
func runLoop(win *render.Window, renderer *render.Renderer, shared *atomic.Pointer[screen.Screen], cfg config.Config, cs *cellSize, cfgPath string, resolve func() config.Config, closeRequested *atomic.Bool, scroll *scrollState, sel *render.Selection, resizeCh chan resizeReq, focused *bool) {
	r := renderer
	var lastScr *screen.Screen
	var lastW, lastH int
	lastScrollLine := -1
	var lastSel render.Selection
	lastFrame := time.Now()
	watch := &cfgWatch{path: cfgPath}
	reload := false
	// lastFocused starts deliberately mismatched against *focused so the
	// branch below always runs (and sets the right SwapInterval) on the
	// very first iteration, regardless of whatever focus state the
	// window happened to open in.
	lastFocused := !*focused

	for !win.ShouldClose() && !closeRequested.Load() {
		if win.GetAttrib(glfw.Iconified) == glfw.True {
			// Truly minimized windows must not burn GPU presenting frames
			// the user can't see at all. WaitEvents blocks (no spin) until
			// the window comes back; the clock is reset so the cursor
			// pulse doesn't jump on restore.
			glfw.WaitEvents()
			lastFrame = time.Now()
			continue
		}
		if *focused != lastFocused {
			// win.SwapBuffers below is vsync'd, and on compositors where a
			// window that's occluded or switched away from (another
			// virtual desktop/workspace, another application) stops
			// receiving frame callbacks, presenting through that
			// unconditionally can block SwapBuffers forever — freezing
			// this whole loop, including glfw.PollEvents, since both run
			// on the single OS-locked thread the window manager expects
			// to keep pumping events. That read as "not responding" until
			// the window manager killed the process.
			//
			// The fix is NOT to stop presenting while unfocused: many
			// window managers (tiling WMs especially) never auto-focus a
			// newly opened window, and a compositor won't even map a
			// surface before its first buffer commit — skipping
			// presentation until focus arrives previously meant the
			// window never appeared at all. Instead, disable vsync while
			// unfocused so SwapBuffers presents immediately without
			// waiting on a frame callback that might never come, and
			// restore normal vsync-paced presentation the instant focus
			// returns. The loop below is throttled via WaitEventsTimeout
			// while unfocused so this doesn't spin unbounded.
			if *focused {
				glfw.SwapInterval(1)
			} else {
				glfw.SwapInterval(0)
			}
			lastFocused = *focused
		}
		if *focused {
			glfw.PollEvents()
		} else {
			glfw.WaitEventsTimeout(1.0 / 30.0)
		}

		// A one-off content-scale check right after window creation isn't
		// always enough — observed on macOS, where GLFW can keep reporting
		// the wrong (1x) scale for a while after the window opens, only
		// correcting itself once some later event (a real resize, entering
		// fullscreen) prods it, or a monitor switch changing it for real.
		// Either way the glyph atlas itself was built for the old scale
		// (see effectiveAtlasScale) and needs rebuilding, not just a
		// proportional resize of cs — a stale-resolution atlas is what
		// used to make text look sharp on one display and soft on
		// another even after cs.w/h caught up. CurrentMonitorContentScale
		// (not the window's own GetContentScale) is what actually makes
		// this catch a plain drag to a differently-scaled monitor: on
		// macOS the window's own cached scale can keep reporting the old
		// monitor's value indefinitely if the user never triggers a real
		// resize/fullscreen afterward, which is exactly the "still
		// degrades on the second monitor" gap the window-level check
		// alone left open. The comparison is two float reads plus a
		// monitor-bounds scan — free next to everything else this loop
		// already does per frame.
		if x, y := win.CurrentMonitorContentScale(); x != cs.dpiX || y != cs.dpiY {
			if nr, err := newRendererFor(win, cfg, cs); err != nil {
				log.Printf("rebuild renderer for display scale change: %v", err)
			} else {
				r = nr
				pushResize(win, resizeCh, cs)
			}
		}

		now := time.Now()
		if watch.changed(now) {
			next := resolve()
			fontChanged := next.Font != cfg.Font || next.Atlas != cfg.Atlas
			cfg = next
			if fontChanged {
				nr, err := newRendererFor(win, cfg, cs)
				if err != nil {
					log.Printf("rebuild renderer for new config: %v", err)
				} else {
					r = nr
					// The window's pixel size didn't change, but cs (cell
					// size) just did — cols/rows must reflow against it, or
					// the grid stays sized for the old font until the user
					// happens to resize the window themselves.
					pushResize(win, resizeCh, cs)
				}
			}
			reload = true
		}
		// dt drives the cursor glide; clamp it so a scheduling stall or
		// compositor hiccup doesn't teleport the cursor across the screen.
		dt := now.Sub(lastFrame).Seconds()
		lastFrame = now
		if dt < 0 {
			dt = 0
		} else if dt > 0.5 {
			dt = 0.5
		}

		w, h := win.FramebufferPixelSize()
		scr := shared.Load()
		if scr.InAltScreen() {
			// A full-screen app took over — viewing scrollback, or a
			// stale local selection from before it started, doesn't
			// make sense under its own live redraws.
			scroll.target = 0
			*sel = render.Selection{}
		}
		r.UpdateScroll(scroll.target, dt)
		scrollLine := r.CurrentScrollLine()
		if scr != lastScr && w == lastW && h == lastH {
			// A genuinely new screen at the same size — apply whatever
			// exact scroll shifts pkg/screen recorded while building it
			// (see Screen.PendingScrolls) so a real terminal scroll (an
			// editor paging, our own scrollback view moving) glides
			// instead of cutting straight to the new state. Empty for any
			// screen that was never a scroll (plain typing, a full
			// repaint) — this is O(1) in that overwhelmingly common case.
			r.ApplyScrollEvents(scr.PendingScrolls(), cs.h)
		}
		r.UpdateContentScroll(dt)

		dirty := scr != lastScr || w != lastW || h != lastH || scrollLine != lastScrollLine || *sel != lastSel || reload || r.ContentScrollActive()
		reload = false

		if dirty {
			r.PrepareFrame(scr, cfg, cs.w, cs.h, scrollLine, *sel)
			r.RenderScene(w, h, cs.w, cs.h, cfg)
		}
		r.UpdateCursor(scr.CursorX, scr.CursorY, scr.CursorVisible, dt)
		r.RenderEffects(w, h, cfg, dt)
		win.SwapBuffers()
		lastScr, lastW, lastH, lastScrollLine, lastSel = scr, w, h, scrollLine, *sel
	}
}
