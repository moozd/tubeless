package tumbler

import (
	"math"
	"testing"
)

// render reads seconds of audio from e as mono float samples (left).
func render(e *Engine, seconds float64) []float64 {
	frames := int(seconds * sampleRate)
	buf := make([]byte, frames*4)
	e.Read(buf)
	out := make([]float64, frames)
	for i := range out {
		out[i] = float64(int16(uint16(buf[i*4])|uint16(buf[i*4+1])<<8)) / 32768
	}
	return out
}

// countClicks counts onsets: a sample crossing the threshold after at
// least 4ms below it.
func countClicks(s []float64) int {
	const gap = sampleRate * 4 / 1000
	n, quiet := 0, gap
	for _, v := range s {
		if math.Abs(v) < 0.05 {
			quiet++
			continue
		}
		if quiet >= gap {
			n++
		}
		quiet = 0
	}
	return n
}

func TestIdleIsSilent(t *testing.T) {
	for _, v := range render(NewEngine(), 0.2) {
		if v != 0 {
			t.Fatal("idle engine produced sound")
		}
	}
}

func TestSingleCellClicksOnce(t *testing.T) {
	e := NewEngine()
	e.Feed(1)
	if got := countClicks(render(e, 0.5)); got != 1 {
		t.Fatalf("one changed cell: %d clicks, want 1", got)
	}
}

func TestMoreChangeSpinsFaster(t *testing.T) {
	slow, fast := NewEngine(), NewEngine()
	slow.Feed(12)
	fast.Feed(120)
	// Same wall time: the bigger diff must get more clicks out of it.
	s, f := countClicks(render(slow, 0.15)), countClicks(render(fast, 0.15))
	if f <= s {
		t.Fatalf("fast spin %d clicks vs slow %d in 150ms", f, s)
	}
}

func TestSteadyStreamMatchesRate(t *testing.T) {
	e := NewEngine()
	var all []float64
	for i := 0; i < 50; i++ { // 1s of frames, 20ms each
		e.Feed(30) // 10 detents per 20ms = 500/s, saturates
		all = append(all, render(e, 0.02)...)
	}
	n := countClicks(all)
	if n < 50 || n > maxRate+10 {
		t.Fatalf("saturated spin made %d clicks in 1s, want ~%d", n, int(maxRate))
	}
}

func TestSpinCoastsToSilence(t *testing.T) {
	e := NewEngine()
	e.Feed(100000)
	render(e, 2.0)
	for _, v := range render(e, 0.2) {
		if v != 0 {
			t.Fatal("still sounding after the backlog should have drained")
		}
	}
}

func TestNoClipOrNaNUnderLoad(t *testing.T) {
	e := NewEngine()
	e.SetVolume(1)
	e.Feed(100000)
	for _, v := range render(e, 1.0) {
		if math.IsNaN(v) || math.Abs(v) > 1 {
			t.Fatalf("bad sample %v", v)
		}
	}
}

func TestClicksStartAndEndAtZero(t *testing.T) {
	for i, c := range renderClicks() {
		if c[0] != 0 || math.Abs(float64(c[len(c)-1])) > 1e-6 {
			t.Fatalf("click %d has an edge step: %v .. %v", i, c[0], c[len(c)-1])
		}
	}
}

func TestVolumeZeroIsSilent(t *testing.T) {
	e := NewEngine()
	e.SetVolume(0)
	e.Feed(300)
	for _, v := range render(e, 0.3) {
		if v != 0 {
			t.Fatal("volume 0 still made sound")
		}
	}
}
