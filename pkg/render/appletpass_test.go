package render

import "testing"

// A 10x4-cell page at col 2, row 3, with 8x16 px cells and the grid
// centered 5,7 px in: every span of it must give the same full rect,
// also when the span is only part of a row or the page is scrolled off
// the top.
func TestPageRect(t *testing.T) {
	layout := appletLayout{cellW: 8, cellH: 16, gridX: 5, gridY: 7}
	tests := []struct {
		name string
		span AppletSpan
		want [4]float32
	}{
		{
			name: "full first row",
			span: AppletSpan{Col: 2, Row: 3, Cells: 10, U0: 0, V0: 0, U1: 1, V1: 0.25},
			want: [4]float32{21, 55, 80, 64},
		},
		{
			name: "middle of a row covered on both sides",
			span: AppletSpan{Col: 5, Row: 5, Cells: 4, U0: 0.3, V0: 0.5, U1: 0.7, V1: 0.75},
			want: [4]float32{21, 55, 80, 64},
		},
		{
			name: "top half scrolled off screen",
			span: AppletSpan{Col: 2, Row: 0, Cells: 10, U0: 0, V0: 0.5, U1: 1, V1: 0.75},
			want: [4]float32{21, -25, 80, 64},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := pageRect(tc.span, layout)
			for i := range got {
				if diff := got[i] - tc.want[i]; diff > 1e-3 || diff < -1e-3 {
					t.Fatalf("pageRect = %v, want %v", got, tc.want)
				}
			}
		})
	}
}
