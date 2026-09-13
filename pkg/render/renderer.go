package render

import (
	"fmt"
	"math"

	"github.com/moozd/tubeless/pkg/config"
	"github.com/moozd/tubeless/pkg/font"
	"github.com/moozd/tubeless/pkg/screen"
)

// Loading is the state for the in-progress overlay: a modal with a title,
// phase label and progress bar, drawn over a dimmed frame while a font or
// atlas rebuild runs off the render thread (see Renderer.SetLoading).
type Loading struct {
	Active bool
	Title  string
	Phase  string
	Done   int
	Total  int
}

// Renderer owns the pipeline that turns the sharp cell grid into the
// tubeless screen:
//
//	(dirty) sceneFBO cleared to opaque black (the empty-terminal base)
//	rect layer (bg fills + solid block glyphs, true rounding) -> shapeFBO (sRGB, transparent)
//	line-art layer (box-drawing/powerline)                     -> shapeFBO (reused after)
//	  each -> BlurPass (gaussian bloom)      -> blurFBO
//	       -> CopyPass.DrawOver (alpha-composite over the base) -> sceneFBO
//	-> sixel images (sharp)                              -> sceneFBO
//	-> underline decorations (sharp)                     -> sceneFBO
//	-> text glyphs (sharp, on top)                        -> sceneFBO
//
//	(every frame) cursor glow                             -> cursorFBO
//	-> InsetPass (cursor soft-add + bg tint + tube falloff) -> default framebuffer
//
// Scene passes are pure functions of the current screen, so they only run
// when the screen/resize changed; the cursor and final inset run every
// visible frame so the cursor glides in real time. There is no decay
// field — phosphor persistence was removed.
type Renderer struct {
	cellPass    *CellPass
	imagePass   *ImagePass
	blurPass    *BlurPass
	copyPass    *CopyPass
	cursorPass  *CursorPass
	insetPass   *InsetPass
	persistPass *PersistPass
	overlayPass *OverlayPass
	shapeFBO    *FBO
	blurFBO     *FBO
	sceneFBO    *FBO
	cursorFBO   *FBO
	persistFBO  [2]*FBO
	persistIdx  int

	// effectsTime is RenderEffects' own wrapped elapsed-seconds clock,
	// feeding the CRT noise/flicker shader effects and the phosphor decay
	// pass — same wrapping pattern as cursorPhase, to avoid ever-growing
	// floating point imprecision over a long-running process.
	effectsTime float64

	cols, rows   int
	cellW, cellH float32

	// Cursor animation state (see UpdateCursor). Position is in grid cell
	// units; phase drives the breathing pulse. Always drawn as exactly
	// one cell (see CursorPass.Draw) — only the position glides between
	// cells, the size never stretches or resizes.
	cursorCol, cursorRow float32
	cursorInit           bool
	cursorVisible        bool
	cursorPhase          float64

	// scrollOffset is the animated (eased) scrollback view position, in
	// lines back from the live tail — see UpdateScroll.
	scrollOffset float32

	pendingImages []screen.PlacedImage

	// loading is the current in-progress overlay state (see SetLoading);
	// when Active, RenderEffects draws the modal over the frame.
	loading Loading
}

// Cursor glide tuning: UpdateCursor's exponential-approach rate (per
// second), and the jump distance (in cells) beyond which the cursor
// snaps instead of gliding.
const (
	// The value from before the elastic front/back trail existed (see
	// git history) — 70, inherited from that trail's "front" role
	// (staying tight to the real cursor through rapid key-repeat
	// retargets), closes ~69% of the gap in a single 60fps frame, which
	// reads as an instant snap rather than a glide now that nothing
	// else draws the motion. 40 still tracks fast typing without
	// visibly lagging behind, while keeping a single deliberate move (an
	// arrow press, a click) a visible glide instead of a snap.
	cursorGlideSpeed = 40.0
	// A scrollback jump or window resize shouldn't animate the cursor
	// "flying" across unrelated content in between.
	cursorSnapDist = 4.0
)

func New(faces *font.Faces, cols, rows int) (*Renderer, error) {
	cellPass, err := NewCellPass(faces)
	if err != nil {
		return nil, fmt.Errorf("cell pass: %w", err)
	}
	imagePass, err := NewImagePass()
	if err != nil {
		return nil, fmt.Errorf("image pass: %w", err)
	}
	blurPass, err := NewBlurPass()
	if err != nil {
		return nil, fmt.Errorf("blur pass: %w", err)
	}
	copyPass, err := NewCopyPass()
	if err != nil {
		return nil, fmt.Errorf("copy pass: %w", err)
	}
	cursorPass, err := NewCursorPass()
	if err != nil {
		return nil, fmt.Errorf("cursor pass: %w", err)
	}
	insetPass, err := NewInsetPass()
	if err != nil {
		return nil, fmt.Errorf("inset pass: %w", err)
	}
	persistPass, err := NewPersistPass()
	if err != nil {
		return nil, fmt.Errorf("persist pass: %w", err)
	}
	overlayPass, err := NewOverlayPass()
	if err != nil {
		return nil, fmt.Errorf("overlay pass: %w", err)
	}
	return &Renderer{
		cellPass:    cellPass,
		imagePass:   imagePass,
		blurPass:    blurPass,
		copyPass:    copyPass,
		cursorPass:  cursorPass,
		insetPass:   insetPass,
		persistPass: persistPass,
		overlayPass: overlayPass,
		shapeFBO:    newSRGBFBO(2, 2),
		blurFBO:     newSRGBFBO(2, 2),
		sceneFBO:    newSRGBFBO(2, 2),
		cursorFBO:   newSRGBFBO(2, 2),
		persistFBO:  [2]*FBO{newFloatFBO(2, 2), newFloatFBO(2, 2)},
	}, nil
}

// PrepareFrame reads scr into GPU-upload-ready instance buffers and records
// the grid/cell geometry the cursor needs. scr should be an immutable
// published snapshot (see cmd/tubeless's publish/load wiring) — this never
// mutates it and needs no locking.
func (r *Renderer) PrepareFrame(scr *screen.Screen, cfg config.Config, cellW, cellH float32, scrollOffset int, sel Selection) {
	r.cellPass.BuildInstances(scr, cfg, cellW, cellH, scrollOffset, sel)
	r.pendingImages = scr.Images
	r.cols, r.rows = scr.Cols, scr.Rows
	r.cellW, r.cellH = cellW, cellH
}

// RenderScene redraws the scene into the scene FBO: an opaque black base,
// then the rect layer (backgrounds + solid block glyphs) and the line-art
// layer (box-drawing/powerline) each drawn transparent, blurred/bloomed,
// and alpha-composited on top in turn — same treatment, so both get a
// soft glow around their true edges without it touching their crisp
// interior — then sharp images and text. These are pure functions of the
// current screen, so they only need to run when the screen/resize
// changed — idle frames reuse the last scene. cellW/cellH are the
// physical-pixel size of one cell (atlas cell size times the display's
// DPI scale).
func (r *Renderer) RenderScene(outW, outH int, cellW, cellH float32, cfg config.Config) {
	scene := r.sceneFBO
	scene.Resize(outW, outH)
	scene.ClearOpaque()
	if cfg.TrueColor {
		r.cellPass.DrawAmbientBG(scene, outW, outH, cfg.Colors.DefaultBg)
	}

	shape := r.shapeFBO
	shape.Resize(outW, outH)

	r.cellPass.DrawRects(shape, cellW, cellH)
	r.compositeGlow(shape, scene, outW, outH, cfg)

	r.cellPass.DrawLineArt(shape, cellW, cellH)
	r.compositeGlow(shape, scene, outW, outH, cfg)

	r.imagePass.Draw(r.sceneFBO, r.pendingImages, cellW, cellH)
	r.cellPass.DrawUnderline(r.sceneFBO, cellW, cellH)
	r.cellPass.DrawText(r.sceneFBO, cellW, cellH)
}

// compositeGlow blurs/blooms src (a transparent layer the caller just drew
// into r.shapeFBO) and alpha-composites the result over dst. Shared by the
// rect and line-art layers in RenderScene, which both draw a crisp,
// premultiplied-alpha shape onto a transparent base and want the same
// soft-glow-around-the-edge treatment — the interior stays exactly as
// drawn (see shapeblur.frag's soft-add) since only the drop in coverage at
// the true edge lets the blurred glow show through.
func (r *Renderer) compositeGlow(src, dst *FBO, outW, outH int, cfg config.Config) {
	if cfg.Blur.Strength > 0.001 && cfg.Blur.Radius > 0.01 {
		r.blurPass.Draw(src, r.blurFBO, cfg.Blur.Radius, cfg.Blur.Strength)
		r.copyPass.DrawOver(r.blurFBO.tex, dst, outW, outH)
	} else {
		r.copyPass.DrawOver(src.tex, dst, outW, outH)
	}
}

// UpdateCursor advances the cursor animation toward the current cell
// (x, y). Called every frame from the render loop. front eases toward the
// real target so small moves (typing, arrow keys) glide while staying
// visually attached to the actual text even under rapid retargets (held
// backspace/arrow-key repeat); back eases toward front rather than the
// raw target, trailing behind it to draw the elastic stretch (see
// CursorPass.Draw). A jump bigger than a few cells — Home/End, or the
// terminal scrolling wholesale — snaps both together to avoid a long
// diagonal flight across the screen.
func (r *Renderer) UpdateCursor(x, y int, visible bool, dt float64) {
	tx, ty := float32(x), float32(y)
	r.cursorVisible = visible
	r.cursorPhase += dt

	if !r.cursorInit {
		r.cursorCol, r.cursorRow = tx, ty
		r.cursorInit = true
		return
	}
	dc := tx - r.cursorCol
	dr := ty - r.cursorRow
	if dc*dc+dr*dr > cursorSnapDist*cursorSnapDist {
		r.cursorCol, r.cursorRow = tx, ty
		return
	}
	k := 1.0 - float32(math.Exp(-cursorGlideSpeed*dt))
	r.cursorCol += (tx - r.cursorCol) * k
	r.cursorRow += (ty - r.cursorRow) * k
}

// scrollEaseSpeed is UpdateScroll's exponential-approach rate (per
// second) — same shape as UpdateCursor's easing, tuned so a wheel notch's jump
// settles in well under 200ms rather than snapping instantly.
const scrollEaseSpeed = 18.0

// UpdateScroll eases the scrollback view position toward target (lines
// back from the live tail), reusing UpdateCursor's exponential-approach
// pattern so a wheel scroll animates smoothly instead of the visible
// window jumping straight to its new position. Called every frame from
// the render loop, same as UpdateCursor.
func (r *Renderer) UpdateScroll(target int, dt float64) {
	k := 1.0 - float32(math.Exp(-scrollEaseSpeed*dt))
	r.scrollOffset += (float32(target) - r.scrollOffset) * k
	if abs32(float32(target)-r.scrollOffset) < 0.05 {
		r.scrollOffset = float32(target)
	}
}

// CurrentScrollLine rounds the animated scroll offset to the nearest
// whole line — the grid can only be drawn at a whole-line offset (see
// screen.Screen.VisibleWindow), so the animation's job is to make the
// sequence of whole-line snaps read as a smooth glide, not to interpolate
// a fractional line itself.
func (r *Renderer) CurrentScrollLine() int {
	return int(math.Round(float64(r.scrollOffset)))
}

// RenderEffects draws the animated cursor glow and presents the final
// composite. It runs every visible frame so the cursor keeps gliding and
// breathing even with the shell idle. dt is the frame's elapsed seconds —
// needed for the CRT noise/flicker effects' time uniform and the phosphor
// decay pass's dt-scaled decay factor, both of which must keep progressing
// at real time regardless of the scene's own dirty/idle state.
func (r *Renderer) RenderEffects(outW, outH int, cfg config.Config, dt float64) {
	offsetX := (float32(outW) - float32(r.cols)*r.cellW) / 2
	offsetY := (float32(outH) - float32(r.rows)*r.cellH) / 2

	// Wrapped at an arbitrary round period (1 day) rather than a
	// meaningful one — unlike cursorPhase, nothing here is periodic on a
	// fixed cycle (noise/flicker just want a monotonically-advancing
	// clock), so this is purely to bound the float64's magnitude over a
	// long-running process, same motivation as cursorPhase's own wrap.
	const effectsTimeWrap = 86400.0
	r.effectsTime = math.Mod(r.effectsTime+dt, effectsTimeWrap)

	// Wrap the accumulated phase down to one period: UpdateCursor just
	// keeps adding dt for the process's whole lifetime, and sin() being
	// periodic makes that mathematically fine, but letting the raw value
	// grow unboundedly for hours eventually loses precision at the scale
	// dt's tiny per-frame increments need, which reads as the breathing
	// cycle's timing drifting/stuttering. Wrapping here bounds the value
	// actually fed to sin() without touching UpdateCursor's signature.
	period := float64(cfg.Cursor.PulsePeriod)
	pulse := 1.0
	if period > 0 {
		r.cursorPhase = math.Mod(r.cursorPhase, period)
		// Gentle breathing: a sine around a floor so the cursor never
		// vanishes while visible — just swells and relaxes. The floor
		// used to live in cursor.frag, where it leaked a ghost whenever a
		// program hid the cursor — the shader now multiplies by uBright
		// directly, so hidden means exactly black.
		pulse = math.Sin(2 * math.Pi * r.cursorPhase / period)
	}
	bright := float32(0.6 + 0.4*pulse)
	bright = 0.35 + 0.65*bright
	if !r.cursorVisible {
		bright = 0
	}
	r.cursorPass.Draw(r.cursorFBO, r.cursorCol, r.cursorRow, offsetX, offsetY, r.cellW, r.cellH, outW, outH, bright, cfg)

	sceneTex := r.sceneFBO.tex
	if decaySeconds := cfg.CRT.PhosphorDecay.DecaySeconds; decaySeconds > 0 {
		var tex uint32
		r.persistIdx, tex = r.persistPass.Step(r.sceneFBO, &r.persistFBO, r.persistIdx, decaySeconds, dt, outW, outH)
		sceneTex = tex
	}
	r.insetPass.Draw(sceneTex, r.cursorFBO.tex, cfg, outW, outH, r.effectsTime)

	if r.loading.Active {
		r.drawOverlay(outW, outH, cfg)
	}
}

// SetLoading replaces the overlay state the next RenderEffects draws. Active
// shows the modal; Done/Total drive the progress bar (Total <= 0 shows an
// indeterminate/empty bar).
func (r *Renderer) SetLoading(l Loading) {
	r.loading = l
}

// drawOverlay renders the loading modal over the just-composited frame: a dim
// backdrop, a centered panel 40% of the window wide, left-aligned title +
// phase text, and a pill progress bar with a percentage. Text samples the
// current atlas (still live during a rebuild); the panel/bar are SDF shapes
// (see OverlayPass).
func (r *Renderer) drawOverlay(outW, outH int, cfg config.Config) {
	cw, ch := r.cellW, r.cellH
	if cw <= 0 || ch <= 0 {
		return
	}
	l := r.loading

	panelW := float32(outW) * 0.4
	panelH := 5.6 * ch
	px := (float32(outW) - panelW) / 2
	py := (float32(outH) - panelH) / 2

	frac := float32(0)
	if l.Total > 0 {
		frac = float32(l.Done) / float32(l.Total)
		frac = max(0, min(1, frac))
	}

	low, high := cfg.Phosphor.Low, cfg.Phosphor.High
	dim := float32(0.5)
	panelColor := [4]float32{low[0] * 0.12, low[1] * 0.12, low[2] * 0.12, 0.97}
	borderColor := [4]float32{low[0] * 0.5, low[1] * 0.5, low[2] * 0.5, 1}
	track := [4]float32{low[0] * 0.4, low[1] * 0.4, low[2] * 0.4, 1}
	fill := [4]float32{high[0], high[1], high[2], 1}
	titleColor := high
	phaseColor := [3]float32{high[0] * 0.7, high[1] * 0.7, high[2] * 0.7}
	pctColor := [3]float32{high[0] * 0.85, high[1] * 0.85, high[2] * 0.85}

	padX := cw
	left := px + padX
	barX := left
	barY := py + 3.0*ch
	barW := panelW - 2*padX
	barH := 0.5 * ch
	radius := float32(10)

	r.overlayPass.Draw(outW, outH,
		[4]float32{px, py, panelW, panelH}, radius, panelColor, borderColor, 1,
		[4]float32{barX, barY, barW, barH}, barH/2, track, fill, frac, dim)

	r.cellPass.DrawTextString(l.Title, left, py+0.5*ch, cw, ch, titleColor, true, float32(outW), float32(outH))
	r.cellPass.DrawTextString(l.Phase, left, py+1.55*ch, cw, ch, phaseColor, false, float32(outW), float32(outH))

	pct := fmt.Sprintf("%d%%", int(frac*100+0.5))
	r.cellPass.DrawTextString(pct, left, py+4.0*ch, cw, ch, pctColor, false, float32(outW), float32(outH))
}
