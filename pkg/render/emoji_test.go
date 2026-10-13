package render

import (
	"testing"

	"github.com/moozd/tubeless/pkg/screen"
)

// emojiRow builds a one-row grid from cells.
func emojiRow(cells ...screen.Cell) [][]screen.Cell {
	return [][]screen.Cell{cells}
}

func TestIsEmojiCandidate(t *testing.T) {
	spacer := screen.Cell{Rune: screen.WideSpacer}
	cases := []struct {
		name string
		grid [][]screen.Cell
		want bool
	}{
		{"wide emoji", emojiRow(screen.Cell{Rune: '🚀'}, spacer), true},
		{"narrow text emoji", emojiRow(screen.Cell{Rune: '❤'}, screen.Cell{Rune: ' '}), false},
		{"vs16 in last column", emojiRow(screen.Cell{Rune: '❤', Tail: "\uFE0F"}), true},
		{"nerd font icon", emojiRow(screen.Cell{Rune: '\uE0B0'}, spacer), false},
	}
	for _, tc := range cases {
		if got := isEmojiCandidate(tc.grid, len(tc.grid[0]), 0, 0); got != tc.want {
			t.Errorf("%s: isEmojiCandidate = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestEmojiQuadCentersSquare(t *testing.T) {
	x, y, size := emojiQuad(emojiCell{x: 3, y: 2, cells: 2}, 10, 24)
	if size != 20 || x != 30 || y != 50 {
		t.Errorf("2-cell quad = (%v, %v, %v), want (30, 50, 20)", x, y, size)
	}
	x, y, size = emojiQuad(emojiCell{x: 0, y: 0, cells: 2}, 20, 24)
	if size != 24 || x != 8 || y != 0 {
		t.Errorf("tall-limited quad = (%v, %v, %v), want (8, 0, 24)", x, y, size)
	}
}
