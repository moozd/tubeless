package tumbler

import "math"

const sampleRate = 48000

// clickLength is how long one detent click rings, in seconds. Every
// component below has decayed to silence well before this, so the tail
// fade at the end only guards against a rounding-level step.
const clickLength = 0.045

// rng is a xorshift64 noise source — deterministic per seed, so the
// bundled clicks sound identical on every run and in every test.
type rng uint64

func (r *rng) next() uint64 {
	x := uint64(*r)
	x ^= x << 13
	x ^= x >> 7
	x ^= x << 17
	*r = rng(x)
	return x
}

// float returns a uniform value in [-1, 1).
func (r *rng) float() float64 {
	return float64(r.next()>>11)/float64(1<<52) - 1
}

// biquad is an RBJ bandpass (constant 0 dB peak gain) filter.
type biquad struct{ b0, b2, a1, a2, z1, z2 float64 }

func newBandpass(freq, q float64) *biquad {
	w := 2 * math.Pi * freq / sampleRate
	alpha := math.Sin(w) / (2 * q)
	a0 := 1 + alpha
	return &biquad{
		b0: alpha / a0,
		b2: -alpha / a0,
		a1: -2 * math.Cos(w) / a0,
		a2: (1 - alpha) / a0,
	}
}

func (f *biquad) process(x float64) float64 {
	y := f.b0*x + f.z1
	f.z1 = f.z2 - f.a1*y
	f.z2 = f.b2*x - f.a2*y
	return y
}

// partial is one ringing resonance of the lock's metal.
type partial struct{ freq, amp, decay float64 }

// metalRing is the pawl and tumbler's own ring — inharmonic, like struck
// steel, with the higher partials dying first.
var metalRing = []partial{
	{1850, 0.34, 0.0050},
	{3120, 0.24, 0.0035},
	{5230, 0.12, 0.0020},
}

// renderClick synthesizes one detent: the pawl dropping into a notch.
// Four layers, summed:
//
//	body   a low thump with a falling pitch — the knob's weight
//	tick   a bright bandpassed noise transient — the pawl's edge
//	ring   decaying inharmonic partials — the steel resonating
//	catch  a softer second tick ~2ms later — the pawl settling
//
// pitch scales every frequency, so one recipe gives a family of clicks.
func renderClick(seed uint64, pitch float64) []float32 {
	n := msToSamples(clickLength * 1000)
	noise := rng(seed)
	tickBand := newBandpass(3300*pitch, 1.3)
	catchBand := newBandpass(4600*pitch, 1.6)
	out := make([]float64, n)
	for i := range out {
		t := float64(i) / sampleRate
		out[i] = bodyAt(t, pitch) + ringAt(t, pitch)
		out[i] += tickBand.process(noise.float()) * math.Exp(-t/0.0007)
		out[i] += 0.55 * catchBand.process(noise.float()) * envelopeAfter(t, 0.0021, 0.0006)
	}
	return finishClick(out)
}

func bodyAt(t, pitch float64) float64 {
	glide := 1 - 0.45*(1-math.Exp(-t/0.012))
	phase := 2 * math.Pi * 170 * pitch * glide * t
	return 0.55 * math.Sin(phase) * math.Exp(-t/0.009)
}

func ringAt(t, pitch float64) float64 {
	var sum float64
	for _, p := range metalRing {
		sum += p.amp * math.Sin(2*math.Pi*p.freq*pitch*t) * math.Exp(-t/p.decay)
	}
	return sum
}

// envelopeAfter is an exponential decay that starts at onset (zero
// before it).
func envelopeAfter(t, onset, decay float64) float64 {
	if t < onset {
		return 0
	}
	return math.Exp(-(t - onset) / decay)
}

// finishClick fades the first and last few samples (so a click can
// never start or end on a step — the classic source of audible pops),
// then normalizes the peak.
func finishClick(raw []float64) []float32 {
	attack := msToSamples(0.2)
	release := msToSamples(6)
	var peak float64
	for i := range raw {
		raw[i] *= edgeFade(i, len(raw), attack, release)
		peak = math.Max(peak, math.Abs(raw[i]))
	}
	out := make([]float32, len(raw))
	for i, v := range raw {
		out[i] = float32(v / peak * 0.9)
	}
	return out
}

func edgeFade(i, n, attack, release int) float64 {
	if i < attack {
		return float64(i) / float64(attack)
	}
	if i >= n-release {
		return float64(n-1-i) / float64(release)
	}
	return 1
}

// clickPitches is the per-variant pitch factor. Real detents are never
// identical — cycling through slightly different clicks is what keeps a
// fast spin from sounding like one sample on repeat (a machine-gun).
var clickPitches = []float64{0.94, 1.02, 0.98, 1.06, 1.00, 0.96, 1.04, 0.99}

func msToSamples(ms float64) int {
	return int(math.Round(ms / 1000 * sampleRate))
}

func renderClicks() [][]float32 {
	clicks := make([][]float32, len(clickPitches))
	for i, p := range clickPitches {
		clicks[i] = renderClick(uint64(0x9E3779B97F4A7C15)*uint64(i+1), p)
	}
	return clicks
}
