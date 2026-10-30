package render

import "testing"

func TestRoundScale(t *testing.T) {
	cases := []struct {
		name           string
		pixels, points int
		want           float32
	}{
		{"retina", 3024, 1512, 2},
		{"standard", 2560, 2560, 1},
		{"mid-resize lag reads as the same scale", 3024, 1513, 2},
		{"fractional", 2880, 1920, 1.5},
	}
	for _, c := range cases {
		if got := roundScale(c.pixels, c.points); got != c.want {
			t.Errorf("%s: roundScale(%d, %d) = %v, want %v", c.name, c.pixels, c.points, got, c.want)
		}
	}
}
