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
	"sync"
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

// sessionRef holds the live *ptyio.Session behind an atomic pointer.
// wireInput/wireMouse register their GLFW callbacks once at startup and
// close over this rather than a raw *ptyio.Session, so their writes keep
// reaching whichever pty child is actually running even after
// ptyCoordinator swaps in a fresh one — see its own doc comment on
// respawning tmux after `tmux kill-server`.
type sessionRef struct {
	p atomic.Pointer[ptyio.Session]
}

func newSessionRef(sess *ptyio.Session) *sessionRef {
	r := &sessionRef{}
	r.p.Store(sess)
	return r
}

func (r *sessionRef) Write(p []byte) (int, error) { return r.p.Load().Write(p) }
func (r *sessionRef) Resize(cols, rows int) error { return r.p.Load().Resize(cols, rows) }
func (r *sessionRef) Close() error                { return r.p.Load().Close() }

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
	themeName := fs.String("theme", "", fmt.Sprintf("starting theme: %s (config file overrides)", strings.Join(config.ThemeNames(), " | ")))
	shell := fs.String("shell", "", "program to run instead of $SHELL")
	fontFamily := fs.String("font", "", "installed font family name (default: bundled FiraCode Nerd)")
	fontSize := fs.Int("font-size", 0, "logical font size in pixels (default: config font.size)")
	fs.Parse(os.Args[1:])

	cfg, cfgPath, err := loadConfig(fs, *themeName, *fontFamily, *fontSize)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	// cfgRef mirrors runLoop's own local cfg for the one consumer outside
	// its single-threaded loop that still needs to read it live: the
	// framebuffer-resize callback (see wireResize/pushResizeSize), which
	// GLFW can fire mid-PollEvents on cfg.CRT.AspectRatio's account —
	// cols/rows must be computed against the letterboxed content box,
	// not the raw window, whenever one is set. Safe to read from that
	// callback without further synchronization: GLFW callbacks fire on
	// the same OS-locked thread runLoop itself runs on (see init's
	// LockOSThread), so there's no concurrent access, just two call
	// sites for the same thread-local value.
	var cfgRef atomic.Pointer[config.Config]
	storeCfgRef(&cfgRef, cfg)
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

	sess := startShell(*shell, cfg.Shell.Program)
	ref := newSessionRef(sess)
	defer ref.Close()
	// log.Fatalf calls os.Exit internally, which would skip the deferred
	// ref.Close() above and leak the already-spawned shell — every fatal
	// error from here on must close it explicitly first.
	fatal := func(format string, args ...any) {
		log.Printf(format, args...)
		ref.Close()
		os.Exit(1)
	}

	// respawn is nil (never restart, just close) for an explicit --shell
	// override or a plain non-tmux shell — the same gating shellCommand
	// itself used to decide sess above. Non-nil, it's what ptyCoordinator
	// calls when the pty's root process exits (e.g. `tmux kill-server`,
	// or the last tmux session ending normally) to bring up a fresh tmux
	// on tmuxSessionName instead of closing the window — see
	// ptyCoordinator's own doc comment.
	var respawn func(cols, rows int) (*ptyio.Session, bool)
	if *shell == "" && cfg.Shell.Program == "tmux" {
		respawn = func(cols, rows int) (*ptyio.Session, bool) {
			name, args, ok := tmuxCommand()
			if !ok {
				return nil, false
			}
			s, err := ptyio.Start(name, args, cols, rows)
			if err != nil {
				log.Printf("restart tmux: %v", err)
				return nil, false
			}
			return s, true
		}
	}

	var shared atomic.Pointer[screen.Screen]
	resizeCh := make(chan resizeReq, 1)
	shared.Store(screen.New(cols, rows))
	go ptyCoordinator(ref, &shared, resizeCh, cfg.Scrollback.Lines, closeRequested, respawn)

	winW := cols * (faces.Regular.CellWidth / effectiveScale) * windowScale
	winH := rows * (faces.Regular.CellHeight / effectiveScale) * windowScale
	win, err := render.NewWindow(windowTitle(""), winW, winH)
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
		w:    physicalCellSize(faces.Regular.CellWidth, effectiveScale, dpiX),
		h:    physicalCellSize(faces.Regular.CellHeight, effectiveScale, dpiY),
		dpiX: dpiX,
		dpiY: dpiY,
	}
	wireResize(win, resizeCh, cs, &cfgRef)
	// GLFW's framebuffer-size callback only fires on a later, real resize
	// — never for the window's initial creation — so without this, cols
	// and rows stay at their fixed startup guess (which the actual
	// framebuffer rarely matches exactly: DPI rounding, the dpiX rebuild
	// above changing the real cell pixel size, or the window manager
	// snapping the requested size) until the user manually resizes and
	// wireResize's callback finally reconciles them. Pushing one resize
	// now, against the window's real framebuffer size and the
	// already-corrected cs, does that reconciliation immediately instead.
	pushResize(win, resizeCh, cs, &cfgRef)

	sel := &render.Selection{}
	fontZoom := make(chan int, 16)
	wireInput(win, ref, &shared, sel, fontZoom)
	scroll := &scrollState{}
	wireMouse(win, ref, &shared, scroll, cs, &cfgRef, sel)

	// focused tracks real window focus, read/written only from this
	// locked OS thread (see runLoop's visibility gate) — no
	// synchronization needed, same as cs above.
	focused := win.GetAttrib(glfw.Focused) == glfw.True
	win.SetFocusCallback(func(_ *glfw.Window, isFocused bool) {
		focused = isFocused
	})

	load := &fontLoad{}
	req, res := startRebuilder(maxTextureSize, load)
	runLoop(win, renderer, &shared, cfg, cs, cfgPath, resolve, closeRequested, scroll, sel, resizeCh, &focused, fontZoom, &cfgRef, req, res, load)
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
		return config.Default(), "", err
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

// storeCfgRef publishes a copy of cfg into ref — see cfgRef's own doc
// comment in main() for why the resize callback needs this instead of
// reading runLoop's local cfg directly.
func storeCfgRef(ref *atomic.Pointer[config.Config], cfg config.Config) {
	c := cfg
	ref.Store(&c)
}

// cellSize is the physical-pixel size of one cell — fixed regardless of
// window size (resizing reflows the grid instead of stretching glyphs),
// but not fixed for the process lifetime: it depends on display content
// scale, which can change (see runLoop's per-frame content-scale check).
// dpiX/dpiY remember the scale the current atlas was actually built for.
type cellSize struct{ w, h, dpiX, dpiY float32 }

// physicalCellSize converts a raster cell dimension (in atlas pixels, at
// effectiveScale) down to a physical display pixel size, rounded to the
// nearest whole pixel. A fractional cell size means every cell boundary
// in the grid lands at a sub-pixel position — px, py := x*cw, y*ch in
// CellPass.BuildInstances compounds that same fractional offset at every
// single cell, so the GPU ends up blending glyph edges across a pixel
// boundary on essentially every cell rather than drawing them crisp,
// which reads as text-wide blur rather than a one-off rounding error.
// The mismatch this rounding introduces against the atlas's own exact
// raster size is at most half a raster pixel — well under one display
// pixel even at a shallow 2x atlas scale — far less visible than a
// misaligned per-cell grid.
func physicalCellSize(rasterPx, effectiveScale int, dpi float32) float32 {
	return float32(math.Round(float64(rasterPx) / float64(effectiveScale) * float64(dpi)))
}

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

// autoAtlasScale picks a base cfg.Atlas.Scale for "auto" (cfg.Atlas.Scale
// <= 0) from the display's own panel class. This isn't just "scale by
// dpi" — effectiveAtlasScale already does that multiplication on top of
// whatever base comes back here.
//
// Tried three values on a real standard-DPI display before landing here.
// 1 (no supersampling at all) skipped uploadAtlas's mipmapped trilinear
// minification entirely — the thing that actually smooths glyph edges,
// not the source rasterization — leaving quality down to FreeType's own
// small-size AA alone, visibly soft. 4 (Retina's own base, tried on the
// theory that matching it would match Retina's quality) tested WORSE,
// not better: glGenerateMipmap in core-profile GL always builds each mip
// level from the previous one (no GENERATE_MIPMAP_HINT in core profile
// to ask the driver for a better filter), so reaching a 4x-to-1x
// minification chains two crude 2x2 box-filter halvings instead of one,
// and that compounded coverage loss read as blur even with atlas.gamma
// back at its 1.0 default — worse than either 1 or 2. 2 is the actual
// sweet spot found by testing: one mip level's worth of minification,
// short of the second halving step that made 4 worse than doing nothing.
// A real fix past this point (matching Retina's actual sharpness on a
// standard-DPI panel) needs a custom downsample pass that bypasses
// glGenerateMipmap's chain entirely, not another number here — Retina
// itself doesn't need one only because its dpi multiplier already buys
// it enough raster detail that the chain's crudeness stops mattering.
func autoAtlasScale(dpi float32) int {
	if dpi < 2 {
		return 2
	}
	return 4
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
	return buildFacesWithProgress(cfg, dpi, maxTextureSize, nil)
}

// buildFacesWithProgress is buildFacesFor with per-glyph progress reporting
// (see font.BuildFaces's Progress) — the async rebuild path reports into a
// fontLoad so the render thread can draw the loading modal.
func buildFacesWithProgress(cfg config.Config, dpi float32, maxTextureSize int, progress font.Progress) (*font.Faces, int, error) {
	requested := cfg.Atlas.Scale
	scale := requested
	if scale <= 0 {
		scale = autoAtlasScale(dpi)
	}
	for {
		effectiveScale := effectiveAtlasScale(scale, dpi)
		if progress != nil && cfg.Font.Family != "" {
			progress("Resolving font", 0, 0)
		}
		faceBytes := loadFontFaces(cfg.Font.Family)
		faces, err := font.BuildFaces(faceBytes, cfg.Font.Size*effectiveScale, cfg.Atlas.Gamma, effectiveScale, cfg.Font.LineHeight, maxTextureSize, cfg.Font.Ligatures, progress)
		if err == nil {
			if requested > 0 && scale != requested {
				log.Printf("atlas.scale %d was too high for this display/GPU; using %d instead — lower it in your config to stop seeing this", requested, scale)
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
//
// respawn is non-nil only when config.Shell.Program == "tmux" launched
// into tmux (see main's own gating on it, which built this closure) — when the
// pty's root process exits (readCh's EOF below), respawn(cols, rows) is
// tried before giving up: a `tmux kill-server`, or the last tmux session
// ending normally, looks identical to ptyCoordinator (its child just
// exited), so both bring up a fresh tmuxSessionName instead of closing
// the window. respawn returning ok=false (tmux no longer on PATH) falls
// through to the same close-the-window path a plain shell exiting always
// took. ref is swapped to the new session so wireInput/wireMouse's
// already-registered callbacks keep writing to whatever's now running.
func ptyCoordinator(ref *sessionRef, shared *atomic.Pointer[screen.Screen], resizeCh <-chan resizeReq, scrollbackLines int, closeRequested *atomic.Bool, respawn func(cols, rows int) (*ptyio.Session, bool)) {
	work := screen.New(cols, rows)
	work.SetScrollbackCap(scrollbackLines)
	handler := screen.NewHandler(work)
	parser := vtparse.New(handler)

	readCh := make(chan []byte)
	go pumpPTYOutput(ref.p.Load(), readCh)

	for {
		select {
		case chunk, ok := <-readCh:
			if !ok {
				if respawn != nil {
					if newSess, ok := respawn(work.Cols, work.Rows); ok {
						ref.p.Store(newSess)
						work.Reset()
						shared.Store(work.Clone())
						readCh = make(chan []byte)
						go pumpPTYOutput(newSess, readCh)
						continue
					}
				}
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
			// also means rendering a rapid sequence of intermediate,
			// half-finished Screens instead of the single complete frame
			// the redraw actually settles into. Draining whatever's already
			// queued, then waiting a short quiet window for more before
			// publishing, coalesces a burst back into that one frame.
			drainPending(readCh, parser)
			for _, resp := range work.DrainResponses() {
				ref.Write(resp)
			}
			shared.Store(work.Clone())
			work.ClearPendingClipboard()
		case req := <-resizeCh:
			if req.cols == work.Cols && req.rows == work.Rows {
				continue
			}
			work.Resize(req.cols, req.rows)
			if err := ref.Resize(req.cols, req.rows); err != nil {
				log.Printf("pty resize: %v", err)
			}
			shared.Store(work.Clone())
			work.ClearPendingClipboard()
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
func wireResize(win *render.Window, resizeCh chan resizeReq, cs *cellSize, cfgRef *atomic.Pointer[config.Config]) {
	win.SetFramebufferSizeCallback(func(_ *glfw.Window, width, height int) {
		c := cfgRef.Load()
		pushResizeSize(resizeCh, cs, width, height, c.CRT.AspectRatio, c.Padding.Size)
	})
}

// pushResize recomputes cols/rows for the window's current framebuffer
// size against cs and sends it — used both by wireResize's callback and
// by the content-scale callback, which needs to reflow immediately when
// cs itself changes rather than waiting for an unrelated resize event.
func pushResize(win *render.Window, resizeCh chan resizeReq, cs *cellSize, cfgRef *atomic.Pointer[config.Config]) {
	w, h := win.FramebufferPixelSize()
	c := cfgRef.Load()
	pushResizeSize(resizeCh, cs, w, h, c.CRT.AspectRatio, c.Padding.Size)
}

// pushResizeSize computes cols/rows against the letterboxed content box
// (render.LetterboxBox), not the raw window — when ar is set, that box
// is smaller than the window (bars around it), so the grid must reflow
// to exactly that many fewer columns/rows at the *same* fixed cell
// pixel size, never a scaled-down cell size squeezed to fit. That's what
// keeps the letterbox from stretching: runLoop renders the scene at
// exactly this box's pixel size (matching what this produces) and
// InsetPass.Draw composites it into that same box at 1:1, no resampling.
// padding shrinks the box cols/rows are fit against (both sides, each
// axis) before it's ever computed — the grid's own centering within the
// full box (see cellpass.go's DrawRects) then turns that shrink into an
// equal margin on every edge, no separate padding-aware draw path needed.
// The send itself is non-blocking and drops a stale pending resize in
// favor of the newest one, so a burst of resize events during a drag
// never backs up.
func pushResizeSize(resizeCh chan resizeReq, cs *cellSize, w, h int, ar config.AspectRatio, padding float32) {
	_, _, bw, bh := render.LetterboxBox(ar, w, h)
	availW := max(0, float32(bw)-2*padding)
	availH := max(0, float32(bh)-2*padding)
	req := resizeReq{cols: max(1, int(availW/cs.w)), rows: max(1, int(availH/cs.h))}
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

// windowTitle is the OS window title to show: appTitle (Screen.Title, an
// app's own OSC 0/2 request — a shell prompt hook, tmux mirroring its
// pane/session title, ssh, ...) verbatim if one has been set, otherwise
// the plain app name. Never a theme-suffixed string — every window
// looked identical in an OS window switcher regardless of what was
// actually running in it.
func windowTitle(appTitle string) string {
	if appTitle != "" {
		return appTitle
	}
	return "tubeless"
}

func startShell(shell, shellProgram string) *ptyio.Session {
	name, args := shellCommand(shell, shellProgram)
	if name == "" {
		name = "/bin/sh"
	}
	sess, err := ptyio.Start(name, args, cols, rows)
	if err != nil {
		log.Fatalf("start shell: %v", err)
	}
	return sess
}

// shellCommand resolves the program+args startShell should launch. An
// explicit --shell override (e.g. tektest for run-green) always wins and
// gets exactly the program named, no injected flag it may not accept.
// Otherwise shellProgram (config.Shell.Program) picks: "" (or "auto")
// follows $SHELL; "tmux" attaches to (or creates) the fixed tmux session,
// falling back to $SHELL if tmux isn't actually on PATH; anything else
// names a shell resolved via PATH (see cmd/tubeless-config's
// shellChoiceNames for what populates that list), falling back to
// $SHELL if it's no longer found there. The plain $SHELL/resolved path
// gets -l (login shell) so .zprofile/.bash_profile PATH setup runs,
// matching every other terminal emulator's default (Terminal.app,
// iTerm, kitty, Alacritty).
func shellCommand(shell, shellProgram string) (string, []string) {
	if shell != "" {
		return shell, nil
	}
	switch shellProgram {
	case "", "auto":
	case "tmux":
		if name, args, ok := tmuxCommand(); ok {
			return name, args
		}
	default:
		if path, err := exec.LookPath(shellProgram); err == nil {
			return path, []string{"-l"}
		}
	}
	return os.Getenv("SHELL"), []string{"-l"}
}

// tmuxSessionName is the fixed session config.Shell.Program == "tmux"
// always attaches to or creates — one persistent session per machine,
// not a fresh one per window.
const tmuxSessionName = "home"

// tmuxCommand resolves the tmux invocation for tmuxSessionName, or
// ok=false if tmux isn't on PATH (shellCommand falls back to the plain
// shell in that case). `new-session -A -s` attaches if the session
// already exists and creates it otherwise, in one atomic step.
//
// Runs through `$SHELL -l -c` rather than exec'ing tmux directly: a login
// shell sources .zprofile/.zshrc, which is where LANG/LC_ALL normally get
// exported. tmux decides once, at its own startup, how to interpret and
// write UTF-8 for its client connection — skipping the login shell means
// tmux launches with whatever locale-less environment a Finder/Dock
// launch gives the GUI process (no LANG at all, on macOS), which can
// mangle Nerd Font icons into "_" even with tmux linked against utf8proc.
// `-c` execs tmux as the shell's last act, so the shell doesn't linger as
// an extra process — tmux still ends up as the pty's direct child.
func tmuxCommand() (name string, args []string, ok bool) {
	if _, err := exec.LookPath("tmux"); err != nil {
		return "", nil, false
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := fmt.Sprintf("exec tmux new-session -A -s %s", tmuxSessionName)
	return shell, []string{"-l", "-c", cmd}, true
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

// installFaces builds a renderer from already-built faces — uploading the
// atlas textures on the render thread — and updates cs with the resulting
// physical cell size. The GL work here is the only part of a font rebuild
// that must stay on the render thread; the rasterization itself runs off
// thread (see startRebuilder).
func installFaces(faces *font.Faces, effectiveScale int, dx, dy float32, cs *cellSize) (*render.Renderer, error) {
	r, err := render.New(faces, cols, rows)
	if err != nil {
		return nil, fmt.Errorf("init renderer: %w", err)
	}
	cs.dpiX, cs.dpiY = dx, dy
	cs.w = physicalCellSize(faces.Regular.CellWidth, effectiveScale, dx)
	cs.h = physicalCellSize(faces.Regular.CellHeight, effectiveScale, dy)
	return r, nil
}

// fontLoad is the shared progress state for an in-flight font/atlas rebuild:
// the background builder writes progress via progress(), the render thread
// reads a snapshot() each frame to draw the loading overlay. gen tags each
// rebuild so a stale result from a superseded build can't clear the overlay
// (or install its faces) while a newer build is still running. Mutex-guarded
// because the builder and render thread run concurrently.
type fontLoad struct {
	mu     sync.Mutex
	active bool
	gen    int
	title  string
	phase  string
	done   int
	total  int
}

// begin marks a new build active and returns its generation.
func (l *fontLoad) begin(title string) int {
	l.mu.Lock()
	l.gen++
	l.active = true
	l.title = title
	l.phase = ""
	l.done, l.total = 0, 0
	g := l.gen
	l.mu.Unlock()
	return g
}

// progress satisfies font.Progress — passed to font.BuildFaces by the
// background builder.
func (l *fontLoad) progress(phase string, done, total int) {
	l.mu.Lock()
	l.phase, l.done, l.total = phase, done, total
	l.mu.Unlock()
}

func (l *fontLoad) clear() {
	l.mu.Lock()
	l.active = false
	l.mu.Unlock()
}

func (l *fontLoad) currentGen() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.gen
}

func (l *fontLoad) snapshot() render.Loading {
	l.mu.Lock()
	defer l.mu.Unlock()
	return render.Loading{Active: l.active, Title: l.title, Phase: l.phase, Done: l.done, Total: l.total}
}

type fontBuildReq struct {
	gen    int
	cfg    config.Config
	dx, dy float32
}

type fontBuildResult struct {
	gen    int
	faces  *font.Faces
	scale  int
	dx, dy float32
	err    error
}

// startRebuilder launches the single background goroutine that builds glyph
// atlases off the render thread. It pulls the latest request from req
// (buffered size 1, so rapid changes coalesce to the newest), reports
// progress into load, and hands the finished faces back on res for the
// render thread to install (the GL upload stays on the render thread). One
// goroutine only: FreeType isn't thread-safe and is only ever used by Build.
func startRebuilder(maxTextureSize int, load *fontLoad) (req chan fontBuildReq, res chan fontBuildResult) {
	req = make(chan fontBuildReq, 1)
	res = make(chan fontBuildResult, 1)
	go func() {
		for r := range req {
			faces, scale, err := buildFacesWithProgress(r.cfg, r.dx, maxTextureSize, load.progress)
			res <- fontBuildResult{gen: r.gen, faces: faces, scale: scale, dx: r.dx, dy: r.dy, err: err}
		}
	}()
	return req, res
}

// requestFontBuild sends a rebuild request, replacing any still-queued one so
// rapid changes (holding Ctrl+=, a monitor switch mid-build) settle on the
// newest config instead of building every intermediate one.
func requestFontBuild(req chan fontBuildReq, r fontBuildReq) {
	select {
	case req <- r:
	default:
		select {
		case <-req:
		default:
		}
		req <- r
	}
}

// fontTitle names the loading modal from the config's font family.
func fontTitle(family string) string {
	if family == "" {
		return "FiraCode Nerd Font Propo"
	}
	return family
}

// drainFontZoom collapses every pending Ctrl/Cmd+=/- press (see
// fontZoomDelta) queued since the last frame into a single net step —
// several rapid presses should feel like one bigger jump, not a
// renderer rebuild per keystroke.
func drainFontZoom(ch <-chan int) int {
	sum := 0
	for {
		select {
		case d := <-ch:
			sum += d
		default:
			return sum
		}
	}
}

// clampFontSize matches the config TUI's own font.size bounds (see
// cmd/tubeless-config) so the live zoom shortcut can't drift outside
// what the config editor itself allows.
func clampFontSize(v int) int {
	return min(96, max(10, v))
}

// zoomedFontSize applies the session's net Ctrl/Cmd+=/- steps (2px each,
// see drainFontZoom) on top of a base size — the same formula runLoop
// uses both when a zoom keypress lands and when reapplying the session's
// zoom onto a freshly disk-resolved config (see fontZoomSteps).
func zoomedFontSize(base, steps int) int {
	return clampFontSize(base + 2*steps)
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
func runLoop(win *render.Window, renderer *render.Renderer, shared *atomic.Pointer[screen.Screen], cfg config.Config, cs *cellSize, cfgPath string, resolve func() config.Config, closeRequested *atomic.Bool, scroll *scrollState, sel *render.Selection, resizeCh chan resizeReq, focused *bool, fontZoom <-chan int, cfgRef *atomic.Pointer[config.Config], req chan fontBuildReq, res chan fontBuildResult, load *fontLoad) {
	r := renderer
	var lastScr *screen.Screen
	lastTitle := windowTitle("")
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
	// reqDpiX/reqDpiY remember the scale the newest font build was
	// *requested* for. cs.dpiX/dpiY only catch up once that build lands and
	// installs, so comparing the monitor's scale against cs below would
	// re-begin a build every single frame for the whole duration of the
	// rebuild — and every begin bumps load.gen, so the in-flight result
	// always came back stale, got dropped, cs never updated, and the
	// loading overlay never cleared after a drag to a differently-scaled
	// display.
	reqDpiX, reqDpiY := cs.dpiX, cs.dpiY

	// fontZoomSteps is the session's live Ctrl/Cmd+=/- zoom, kept only in
	// memory and never written through resolve()/config.Save — it must
	// survive a watch.changed reload (someone editing an unrelated
	// setting in tubeless-config, or the file touched by hand) instead of
	// being silently discarded when cfg gets replaced wholesale by
	// whatever the file resolves to.
	fontZoomSteps := 0

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
		if x, y := win.CurrentMonitorContentScale(); x != reqDpiX || y != reqDpiY {
			reqDpiX, reqDpiY = x, y
			g := load.begin("Display scale change")
			requestFontBuild(req, fontBuildReq{gen: g, cfg: cfg, dx: x, dy: y})
		}

		if d := drainFontZoom(fontZoom); d != 0 {
			fontZoomSteps += d
			cfg.Font.Size = zoomedFontSize(cfg.Font.Size, d)
			storeCfgRef(cfgRef, cfg)
			g := load.begin(fmt.Sprintf("Font size %dpx", cfg.Font.Size))
			requestFontBuild(req, fontBuildReq{gen: g, cfg: cfg, dx: reqDpiX, dy: reqDpiY})
			reload = true
		}

		now := time.Now()
		if watch.changed(now) {
			next := resolve()
			// Reapply the session's own zoom on top of the freshly
			// resolved base size, not the previous (already-zoomed)
			// cfg.Font.Size — otherwise an unrelated config edit would
			// double-apply the zoom (or drop it, if the file's own
			// font.size happened to already match cfg.Font.Size).
			next.Font.Size = zoomedFontSize(next.Font.Size, fontZoomSteps)
			fontChanged := next.Font != cfg.Font || next.Atlas != cfg.Atlas
			// A pure aspect-ratio or padding edit (no font/atlas change)
			// still needs cols/rows recomputed against the new letterboxed
			// content box (see pushResizeSize) — just not a full
			// renderer/atlas rebuild.
			arChanged := next.CRT.AspectRatio != cfg.CRT.AspectRatio
			paddingChanged := next.Padding != cfg.Padding
			cfg = next
			storeCfgRef(cfgRef, cfg)
			switch {
			case fontChanged:
				g := load.begin(fontTitle(cfg.Font.Family))
				requestFontBuild(req, fontBuildReq{gen: g, cfg: cfg, dx: reqDpiX, dy: reqDpiY})
			case arChanged, paddingChanged:
				pushResize(win, resizeCh, cs, cfgRef)
			}
			reload = true
		}

		// A finished background build lands here. If it's the latest request
		// (gen still current), install its faces on this thread (the GL
		// upload) and reflow the grid, then clear the overlay; a stale result
		// from a superseded build is dropped — the newer one, already running
		// or queued, installs instead. The terminal stays live throughout.
		select {
		case result := <-res:
			if result.gen == load.currentGen() {
				load.clear()
				if result.err != nil {
					log.Printf("rebuild font: %v", result.err)
				} else if nr, err := installFaces(result.faces, result.scale, result.dx, result.dy, cs); err != nil {
					log.Printf("rebuild renderer: %v", err)
				} else {
					r = nr
					pushResize(win, resizeCh, cs, cfgRef)
				}
				reload = true
			}
		default:
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
		if scr != lastScr {
			// A yank in tmux (with set-clipboard on) or an OSC52-aware
			// app relaying a copy out of a nested session — see
			// Screen.PendingClipboard — lands here as plain text to
			// forward to the real system clipboard. Gated on scr !=
			// lastScr (not size, unlike the shift detectors below) since
			// a clipboard set has nothing to do with grid geometry and
			// should never be missed just because a resize also landed
			// this frame.
			if sets := scr.PendingClipboard(); len(sets) > 0 {
				writeClipboard(win, sets[len(sets)-1])
			}
			if title := windowTitle(scr.Title); title != lastTitle {
				win.SetTitle(title)
				lastTitle = title
			}
		}
		dirty := scr != lastScr || w != lastW || h != lastH || scrollLine != lastScrollLine || *sel != lastSel || reload
		reload = false

		// Computed every frame, not just when dirty — RenderEffects below
		// needs it every frame (the cursor glow and phosphor persistence
		// FBOs must stay sized to this box, matching the scene texture,
		// even on a frame that only re-presents an unchanged scene) to
		// keep that texture composited at 1:1, never resampled to a
		// different size (see RenderEffects's own doc comment on boxW/
		// boxH — that resample is what used to read as blurry text
		// whenever the aspect ratio didn't match the window's own shape).
		_, _, bw, bh := render.LetterboxBox(cfg.CRT.AspectRatio, w, h)
		if dirty {
			r.PrepareFrame(scr, cfg, cs.w, cs.h, scrollLine, *sel)
			r.RenderScene(bw, bh, cs.w, cs.h, cfg)
		}
		r.UpdateCursor(scr.CursorX, scr.CursorY, scr.CursorVisible, dt)
		r.SetLoading(load.snapshot())
		r.RenderEffects(bw, bh, w, h, cfg, dt)
		win.SwapBuffers()
		lastScr, lastW, lastH, lastScrollLine, lastSel = scr, w, h, scrollLine, *sel
	}
}
