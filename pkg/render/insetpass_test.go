package render

import "testing"

func TestAmbientColorIsNearNeutralGray(t *testing.T) {
	amber := [3]float32{1, 0.5, 0}
	c := ambientColor(0.04, amber)
	if c[0] != 0.04 {
		t.Fatalf("hue channel = %v, want the full amount 0.04", c[0])
	}
	if c[2] < 0.04*(1-ambientCast)-1e-6 {
		t.Fatalf("off-hue channel = %v, want at least %v", c[2], 0.04*(1-ambientCast))
	}
	if !(c[0] > c[1] && c[1] > c[2]) {
		t.Fatalf("ambient %v does not lean toward amber", c)
	}
}

func TestAmbientColorOff(t *testing.T) {
	if c := ambientColor(0, [3]float32{1, 0.5, 0}); c != ([3]float32{}) {
		t.Fatalf("ambient 0 = %v, want black", c)
	}
	if c := ambientColor(0.03, [3]float32{}); c != ([3]float32{0.03, 0.03, 0.03}) {
		t.Fatalf("black accent = %v, want neutral gray", c)
	}
}
