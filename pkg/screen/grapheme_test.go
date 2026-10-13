package screen

import "testing"

// rowText renders row y as the text a reader sees: each cell's cluster,
// with WideSpacer cells shown as "_".
func rowText(s *Screen, y int) string {
	out := ""
	for _, c := range s.Grid[y] {
		if c.Rune == WideSpacer {
			out += "_"
			continue
		}
		out += c.Text()
	}
	return out
}

func TestGraphemeLayout(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		cursorX int
	}{
		{"emoji takes two columns", "🚀|", "🚀_|   ", 3},
		{"cjk takes two columns", "中a", "中_a   ", 3},
		{"vs16 widens a text emoji", "❤\uFE0F|", "❤\uFE0F_|   ", 3},
		{"bare text emoji stays narrow", "❤|", "❤|    ", 2},
		{"skin tone joins its base", "👍🏽|", "👍🏽_|   ", 3},
		{"zwj family is one cell", "👨\u200D👩\u200D👧|", "👨\u200D👩\u200D👧_|   ", 3},
		{"flag pair is one cell", "🇳🇱|", "🇳🇱_|   ", 3},
		{"combining accent joins", "e\u0301x", "e\u0301x    ", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, s := newTestParser(6, 2)
			p.Write([]byte(tc.input))
			if got := rowText(s, 0); got != tc.want {
				t.Errorf("row = %q, want %q", got, tc.want)
			}
			if s.CursorX != tc.cursorX {
				t.Errorf("CursorX = %d, want %d", s.CursorX, tc.cursorX)
			}
		})
	}
}

func TestWideRuneWrapsAtLastColumn(t *testing.T) {
	p, s := newTestParser(3, 2)
	p.Write([]byte("ab🚀"))
	if got := rowText(s, 0); got != "ab " {
		t.Errorf("row 0 = %q, want %q", got, "ab ")
	}
	if got := rowText(s, 1); got != "🚀_ " {
		t.Errorf("row 1 = %q, want %q", got, "🚀_ ")
	}
}

func TestOverwritingHalfOfWideCellBlanksTheOther(t *testing.T) {
	p, s := newTestParser(4, 1)
	p.Write([]byte("🚀\x1b[1;2Hx"))
	if got := rowText(s, 0); got != " x  " {
		t.Errorf("spacer overwrite: row = %q, want %q", got, " x  ")
	}
	p.Write([]byte("\x1b[1;1H🚀\x1b[1;1Hy"))
	if got := rowText(s, 0); got != "y   " {
		t.Errorf("owner overwrite: row = %q, want %q", got, "y   ")
	}
}
