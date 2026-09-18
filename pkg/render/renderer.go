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
//	-> bg fills + block glyphs (literal cells)            -> effectFBO
//	-> box-drawing/powerline glyphs (literal glyphs)       -> effectFBO
//	-> fragment-space surface radius                      -> surfaceFBO
//	-> optional dreamy drop shadow over surfaceFBO's rounded shape -> shadowFBO
//	-> optional bloom over surfaceFBO/shadowFBO             -> sceneFBO
//	-> sixel images                                       -> sceneFBO
//	-> underline decorations                              -> sceneFBO
//	-> text glyphs                                        -> sceneFBO
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
	surfacePass *SurfacePass
	shadowPass  *ShadowPass
	copyPass    *CopyPass
	cursorPass  *CursorPass
	insetPass   *InsetPass
	persistPass *PersistPass
	overlayPass *OverlayPass
	effectFBO   *FBO
	surfaceFBO  *FBO
	shadowFBO   *FBO
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
	// units; phase drives the breathing pulse. The head is always drawn
	// within one cell's footprint (see CursorPass.Draw) — only its shape,
	// not its footprint, reacts to speed.
	cursorCol, cursorRow float32
	cursorInit           bool
	cursorVisible        bool
	cursorPhase          float64

	// Speed-reactive ball+tail state (see UpdateCursor). cursorSpeed is
	// the eased glide's own smoothed speed, in cells/sec; cursorDirX/Y is
	// the unit direction (grid-cell units) it was last moving in, held
	// steady while stopped so a settled tail doesn't snap direction;
	// cursorMorph is the eased 0..1 block-to-ball blend that cursorSpeed
	// drives via cursorMorphSpeedLow..High.
	cursorSpeed            float32
	cursorDirX, cursorDirY float32
	cursorMorph            float32

	// scrollOffset is the animated (eased) scrollback view position, in
	// lines back from the live tail — see UpdateScroll.
	scrollOffset float32

	pendingImages []screen.PlacedImage

	// loading is the current in-progress overlay state (see SetLoading);
	// when Active, RenderEffects draws the modal over the frame.
	loading Loading
}

// Cursor glide tuning: UpdateCursor's exponential-approach rate (per
// second). There is no distance-based snap — every move, from a single
// typed character to a full jump across the buffer (:, gg, G, a search
// result, a click), glides through the same easing; a bigger distance
// covered in the same real-time step just reads as a higher instantaneous
// speed, which is exactly what the ball/tail morph below reacts to.
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

	// cursorSpeedSmooth smooths the glide's own frame-to-frame speed
	// (cells/sec), which config.Trail's SpeedLow/SpeedHigh then map into
	// a 0..1 "how ball-like" target (see UpdateCursor) — so the shape
	// change itself reads as a morph in both directions (speeding into a
	// ball, slowing back into the plain block) rather than a cut.
	//
	// Trail's own SpeedLow default sits well above ordinary typing and
	// held-key repeat: at a steady 60fps, one frame's glide covers
	// roughly distance*29 cells/sec (cursorGlideSpeed's k, divided by
	// dt) — so advancing one cell per retarget (typing, arrow-key
	// repeat, even a fast ~50cps key-repeat rate) tops out well under 45
	// and never morphs, while a real jump of several cells or more in
	// one frame clears it easily and heads straight for the ball.
	cursorSpeedSmooth = 20.0
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
	surfacePass, err := NewSurfacePass()
	if err != nil {
		return nil, fmt.Errorf("surface pass: %w", err)
	}
	shadowPass, err := NewShadowPass()
	if err != nil {
		return nil, fmt.Errorf("shadow pass: %w", err)
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
		surfacePass: surfacePass,
		shadowPass:  shadowPass,
		copyPass:    copyPass,
		cursorPass:  cursorPass,
		insetPass:   insetPass,
		persistPass: persistPass,
		overlayPass: overlayPass,
		effectFBO:   newSRGBFBO(2, 2),
		surfaceFBO:  newSRGBFBO(2, 2),
		shadowFBO:   newSRGBFBO(2, 2),
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

// RenderScene redraws the terminal's literal image into the scene FBO:
// backgrounds, block glyphs, line art, images, underlines and text are
// drawn exactly as the screen model describes them. Surface radius and bloom
// use a second literal source containing only solid/border buckets, then
// composite that filtered image under images/underlines/text. No pass infers
// surfaces from neighboring cells or reinterprets escape-sequence output.
func (r *Renderer) RenderScene(outW, outH int, cellW, cellH float32, cfg config.Config) {
	scene := r.sceneFBO
	scene.Resize(outW, outH)
	scene.ClearOpaque()
	if cfg.TrueColor {
		r.cellPass.DrawAmbientBG(scene, outW, outH, cfg.Colors.DefaultBg)
	}

	effect := r.effectFBO
	effect.Resize(outW, outH)
	effect.Clear()
	r.cellPass.DrawRects(effect, cellW, cellH)
	r.cellPass.DrawLineArt(effect, cellW, cellH)

	surfaceTex := effect.tex
	if cfg.Surface.Radius > 0.01 || cfg.Surface.Gradient > 0.001 {
		r.surfacePass.Draw(effect, r.surfaceFBO, cfg.Surface.Radius, cfg.Surface.Gradient)
		surfaceTex = r.surfaceFBO.tex
	}
	if cfg.Surface.Shadow > 0.001 {
		r.shadowPass.Draw(surfaceTex, r.shadowFBO, outW, outH, cfg.Surface.Shadow, cfg.Surface.Radius)
		surfaceTex = r.shadowFBO.tex
	}
	r.copyPass.DrawOver(surfaceTex, scene, outW, outH)

	if cfg.Blur.Strength > 0.001 && cfg.Blur.Radius > 0.01 {
		r.blurPass.DrawTex(surfaceTex, r.blurFBO, cfg.Blur.Radius, cfg.Blur.Strength, outW, outH)
		r.copyPass.DrawOver(r.blurFBO.tex, scene, outW, outH)
	}
	r.imagePass.Draw(r.sceneFBO, r.pendingImages, cellW, cellH)
	r.cellPass.DrawUnderline(r.sceneFBO, cellW, cellH)
	r.cellPass.DrawText(r.sceneFBO, cellW, cellH)
}

// UpdateCursor advances the cursor animation toward the current cell
// (x, y). Called every frame from the render loop. The position eases
// exponentially toward the real target, whatever the distance — typing,
// arrow keys, and a full jump across the buffer (:, gg, G, a search
// result, a click) all glide through the same easing rather than some of
// them teleporting. Exponential easing converges to "close enough" in
// roughly the same wall-clock time regardless of distance (it closes a
// fixed percentage of the remaining gap per unit time, not a fixed
// distance), so a big jump still settles quickly — it just does so while
// visibly flying across the intervening cells, by design (see
// trail's SpeedLow/SpeedHigh below). Alongside the position, this also
// tracks the glide's own frame-to-frame speed and eases cursorMorph
// toward the ball/tail shape it drives (see CursorPass.Draw /
// cursor.frag) — an animation layered entirely on top of the existing
// glide, using no state the glide didn't already have. trail.Enabled ==
// false skips the morph entirely, snapping cursorMorph straight to 0 so
// the cursor stays its plain at-rest Shape regardless of glide speed.
func (r *Renderer) UpdateCursor(x, y int, visible bool, dt float64, trail config.Trail) {
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
	k := 1.0 - float32(math.Exp(-cursorGlideSpeed*dt))
	moveCol, moveRow := dc*k, dr*k
	r.cursorCol += moveCol
	r.cursorRow += moveRow

	if dt <= 0 {
		return
	}
	dist := float32(math.Hypot(float64(moveCol), float64(moveRow)))
	ks := 1.0 - float32(math.Exp(-cursorSpeedSmooth*dt))
	r.cursorSpeed += (dist/float32(dt) - r.cursorSpeed) * ks
	if dist > 1e-4 {
		r.cursorDirX, r.cursorDirY = moveCol/dist, moveRow/dist
	}

	if !trail.Enabled {
		r.cursorMorph = 0
		return
	}
	target := smoothstep32(trail.SpeedLow, trail.SpeedHigh, r.cursorSpeed)
	km := 1.0 - float32(math.Exp(-float64(trail.Ease)*dt))
	r.cursorMorph += (target - r.cursorMorph) * km
}

// cursorBrightness computes the cursor's uBright intensity for the
// configured blink style. "ease" (default) breathes via a sine floor/
// ceiling (0.35..1); "static" stays fully lit with no pulse; "hard"
// toggles fully on/off each half-period, like a classic terminal cursor.
// phase is UpdateCursor's running clock, already wrapped to one period by
// the caller; period <= 0 reads as "static" regardless of style, same as
// an unset pulse always did.
func cursorBrightness(style string, phase, period float64, visible bool) float32 {
	if !visible {
		return 0
	}
	if style == "static" || period <= 0 {
		return 1
	}
	if style == "hard" {
		if phase < period/2 {
			return 1
		}
		return 0
	}
	pulse := math.Sin(2 * math.Pi * phase / period)
	bright := 0.6 + 0.4*pulse
	return float32(0.35 + 0.65*bright)
}

// smoothstep32 mirrors GLSL's smoothstep: a hermite curve that maps x into
// 0..1 across edge0..edge1 with zero slope at both ends, so the speed-to-
// morph mapping eases in and out instead of ramping linearly.
func smoothstep32(edge0, edge1, x float32) float32 {
	t := (x - edge0) / (edge1 - edge0)
	t = float32(math.Max(0, math.Min(1, float64(t))))
	return t * t * (3 - 2*t)
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
//
// boxW/boxH is the letterboxed content box RenderScene rendered the scene
// texture at (see cmd/tubeless's LetterboxBox call) — every FBO here that
// samples or accumulates that scene texture (the cursor glow FBO, the
// phosphor persistence accumulator) must be sized to match it exactly,
// not outW/outH. Sizing them to the raw window instead silently resamples
// the box-sized scene up to the window size and back down again in
// InsetPass.Draw below, which reads as blurry text — but only once the
// letterbox box stops matching the window (an aspect ratio that doesn't
// match the window's own shape), which is exactly why this bug tracked
// with aspect ratio and stayed invisible at a matching fullscreen size.
// outW/outH stays the true window size only for InsetPass.Draw and
// drawOverlay, since those two draw straight to the real default
// framebuffer and need to know its real dimensions to place the
// letterbox bars/modal correctly.
func (r *Renderer) RenderEffects(boxW, boxH, outW, outH int, cfg config.Config, dt float64) {
	offsetX, offsetY := gridOffset(float32(boxW), float32(boxH), r.cols, r.rows, r.cellW, r.cellH)

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
	if period > 0 {
		r.cursorPhase = math.Mod(r.cursorPhase, period)
	}
	// Glass mode reads as a static panel, not an animated cursor — see
	// config.Glass's own doc comment — so it overrides BlinkStyle
	// (ignoring PulsePeriod entirely) and the speed-reactive ball/tail
	// morph, regardless of what glide speed UpdateCursor is tracking.
	style := cfg.Cursor.BlinkStyle
	morph := r.cursorMorph
	if cfg.Cursor.Glass.Enabled {
		style = "static"
		morph = 0
	}
	bright := cursorBrightness(style, r.cursorPhase, period, r.cursorVisible)

	// The tail direction is tracked in grid-cell units (see UpdateCursor)
	// but cells aren't necessarily square, so it's rescaled into screen
	// pixels here before handing it to the shader — otherwise a diagonal
	// glide's tail would point off at the wrong angle whenever cellW and
	// cellH differ.
	tailPxX, tailPxY := -r.cursorDirX*r.cellW, -r.cursorDirY*r.cellH
	if tailPxLen := float32(math.Hypot(float64(tailPxX), float64(tailPxY))); tailPxLen > 1e-4 {
		tailPxX, tailPxY = tailPxX/tailPxLen, tailPxY/tailPxLen
	}
	tailLenPx := morph * cfg.Cursor.Trail.MaxCells * (r.cellW + r.cellH) * 0.5

	r.cursorPass.Draw(r.cursorFBO, r.cursorCol, r.cursorRow, offsetX, offsetY, r.cellW, r.cellH, boxW, boxH, bright, morph, tailPxX, tailPxY, tailLenPx, r.sceneFBO.tex, cfg)

	sceneTex := r.sceneFBO.tex
	if decaySeconds := cfg.CRT.PhosphorDecay.DecaySeconds; decaySeconds > 0 {
		var tex uint32
		r.persistIdx, tex = r.persistPass.StepTex(sceneTex, &r.persistFBO, r.persistIdx, decaySeconds, dt, boxW, boxH)
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
