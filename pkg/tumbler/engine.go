// Package tumbler is the safe-dial sound effect: screen changes turn an
// imaginary combination-lock knob, and every detent it passes is a
// synthesized mechanical click. The more content changes, the faster
// the knob spins — a lone keystroke is one click, a full redraw is a
// hard spin that coasts down.
//
// Engine is the pure part (feed it change counts, read PCM); Player
// (player.go) owns the audio device.
package tumbler

import (
	"math"
	"sync"
)

const (
	// cellsPerDetent is how many changed cells turn the knob one click.
	cellsPerDetent = 3.0
	// clickThreshold is the backlog (in detents) needed to fire a
	// click — low enough that a single changed cell still clicks.
	clickThreshold = 0.25
	// maxPending bounds the backlog so a huge redraw spins hard for
	// about a second instead of for as long as the diff was large.
	maxPending = 48.0
	// drainTau relates backlog to speed: clicks per second =
	// backlog / drainTau, so a steady stream of D detents/s settles
	// at exactly D clicks/s and the knob speed tracks the diff rate.
	drainTau = 0.06
	// minRate/maxRate bound the click rate (per second). The floor
	// lets a small backlog finish promptly; the ceiling keeps the
	// fastest spin a ratchet instead of a buzz.
	minRate = 14.0
	maxRate = 75.0
	// maxVoices caps overlapping clicks.
	maxVoices = 32
)

// voice is one click being played back.
type voice struct {
	buf         []float32
	pos, step   float64
	left, right float32
}

// Engine turns change counts into PCM. Safe for one writer (Feed) and
// one reader (Read) on different goroutines.
type Engine struct {
	mu      sync.Mutex
	clicks  [][]float32
	voices  []voice
	volume  float64
	pending float64
	phase   float64
	variant int
	noise   rng
}

// NewEngine renders the click set and returns an idle engine.
func NewEngine() *Engine {
	return &Engine{clicks: renderClicks(), volume: 0.5, phase: 1, noise: 0xC0FFEE}
}

// SetVolume sets the master gain, clamped to 0-1.
func (e *Engine) SetVolume(v float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.volume = math.Max(0, math.Min(1, v))
}

// Feed turns the knob by changedCells cells' worth of screen change.
func (e *Engine) Feed(changedCells int) {
	if changedCells <= 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pending = math.Min(maxPending, e.pending+float64(changedCells)/cellsPerDetent)
}

// Read fills p with signed 16-bit little-endian stereo PCM. It never
// blocks and always fills p (silence when idle), so it can sit directly
// behind an audio device.
func (e *Engine) Read(p []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	frames := len(p) / 4
	for i := 0; i < frames; i++ {
		e.step()
		l, r := e.mix()
		putSample(p[i*4:], l)
		putSample(p[i*4+2:], r)
	}
	return frames * 4, nil
}

// step advances the knob one sample: accumulates rotation phase at the
// current speed and fires a click on each detent crossing. Timing is
// sample-accurate and evenly spaced, so a spin never stutters.
func (e *Engine) step() {
	rate := e.rate()
	e.phase = math.Min(1, e.phase+rate/sampleRate)
	if e.phase < 1 || e.pending < clickThreshold {
		return
	}
	e.phase = 0
	e.pending = math.Max(0, e.pending-1)
	e.fire(rate)
}

// rate is the knob speed in clicks per second.
func (e *Engine) rate() float64 {
	return math.Max(minRate, math.Min(maxRate, e.pending/drainTau))
}

// fire starts one click. Speed shapes it: a fast spin plays each click
// higher, shorter and softer (a ratchet), a slow turn heavy and full.
func (e *Engine) fire(rate float64) {
	speed := (rate - minRate) / (maxRate - minRate)
	e.variant = (e.variant + 1) % len(e.clicks)
	pan := 0.15 * e.noise.float()
	jitter := 1 + 0.04*e.noise.float()
	gain := (1 - 0.35*speed) * (1 + 0.08*e.noise.float())
	v := voice{
		buf:   e.clicks[e.variant],
		step:  (0.94 + 0.3*speed) * jitter,
		left:  float32(gain * math.Cos(math.Pi/4*(1+pan))),
		right: float32(gain * math.Sin(math.Pi/4*(1+pan))),
	}
	if len(e.voices) >= maxVoices {
		e.voices = e.voices[1:]
	}
	e.voices = append(e.voices, v)
}

// mix sums every live voice for one sample (linear interpolation, since
// a voice's step is rarely 1) and soft-limits the result.
func (e *Engine) mix() (l, r float32) {
	if len(e.voices) == 0 {
		return 0, 0
	}
	live := e.voices[:0]
	for _, v := range e.voices {
		i := int(v.pos)
		if i+1 >= len(v.buf) {
			continue
		}
		frac := float32(v.pos - float64(i))
		s := v.buf[i]*(1-frac) + v.buf[i+1]*frac
		l += s * v.left
		r += s * v.right
		v.pos += v.step
		live = append(live, v)
	}
	e.voices = live
	g := e.volume * 1.4
	return softLimit(float64(l) * g), softLimit(float64(r) * g)
}

func softLimit(x float64) float32 { return float32(math.Tanh(x)) }

func putSample(b []byte, s float32) {
	v := int16(s * 32767)
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}
