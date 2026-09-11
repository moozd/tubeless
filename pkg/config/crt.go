package config

// CRT groups every optional CRT-emulation effect beyond the always-on
// tube-face chrome (Blur/Face/Cursor above). Every field's Go zero value
// means "off" — matching Blur.Strength/Face.BgTint/Cursor.Glow's existing
// convention rather than a separate Enabled flag — so a new CRT field
// needs no edits to any of the 10 preset functions, and every preset ships
// with all of these off by default.
type CRT struct {
	Curvature     Curvature     `toml:"curvature"`
	Scanlines     Scanlines     `toml:"scanlines"`
	Aberration    Aberration    `toml:"aberration"`
	ShadowMask    ShadowMask    `toml:"shadow_mask"`
	Noise         Noise         `toml:"noise"`
	Flicker       Flicker       `toml:"flicker"`
	PhosphorDecay PhosphorDecay `toml:"phosphor_decay"`
	AspectRatio   AspectRatio   `toml:"aspect_ratio"`
}

// AspectRatio locks the rendered picture to a fixed width:height ratio —
// a real CRT has a fixed tube shape, unlike a freely resizable terminal
// window. When set (both fields >0), the render pass (see
// pkg/render/insetpass.go) letterboxes or pillarboxes: it draws the
// whole scene, uniformly scaled to fit, into a centered box of that
// ratio within the window, with black bars filling the rest — it never
// crops or distorts the terminal grid, which is still laid out to fill
// the actual window regardless of this setting. Width/Height both 0
// (every non-monitor preset, "modern" included) fills the window
// exactly, as before.
type AspectRatio struct {
	Width  float32 `toml:"width"`
	Height float32 `toml:"height"`
}

// Curvature bends the rendered content toward a barrel-distorted tube
// face. 0 is flat; ~0.15 is a subtle curve, ~0.4 a strong one — beyond
// that, text near the corners starts to clip against the curved edge.
type Curvature struct {
	Amount float32 `toml:"amount"`
}

// Scanlines darkens alternating horizontal lines, like an interlaced CRT's
// visible raster. Period is in device pixels per line-pair; only read
// once Intensity > 0.
type Scanlines struct {
	Intensity float32 `toml:"intensity"`
	Period    float32 `toml:"period"`
}

// Aberration offsets the red/blue channels outward from center (a lens's
// chromatic aberration), in UV units. 0 is off.
type Aberration struct {
	Amount float32 `toml:"amount"`
}

// ShadowMask overlays a procedural RGB triad pattern, like a shadow-mask
// CRT's subpixel structure. CellSize is device pixels per triad column;
// only read once Intensity > 0.
type ShadowMask struct {
	Intensity float32 `toml:"intensity"`
	CellSize  float32 `toml:"cell_size"`
}

// Noise adds a faint per-pixel, per-frame brightness jitter — analog
// signal noise. 0 is off.
type Noise struct {
	Intensity float32 `toml:"intensity"`
}

// Flicker varies the whole screen's brightness slowly over time, like an
// unstable power supply. Speed is in Hz-ish; only read once Amount > 0.
type Flicker struct {
	Amount float32 `toml:"amount"`
	Speed  float32 `toml:"speed"`
}

// PhosphorDecay trails an afterglow behind content that just changed,
// decaying toward black — real phosphor's persistence after the electron
// beam moves on. DecaySeconds is roughly how long a full-brightness pixel
// takes to fade to ~5% residual; 0 disables it (no accumulator, no cost).
type PhosphorDecay struct {
	DecaySeconds float32 `toml:"decay_seconds"`
}
