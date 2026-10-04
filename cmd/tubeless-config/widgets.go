package main

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

// SGR helpers.
const (
	sgrReset   = "\x1b[0m"
	sgrBold    = "\x1b[1m"
	sgrDim     = "\x1b[2m"
	sgrReverse = "\x1b[7m"
)

// cells composes one row of terminal text while tracking its on-screen
// width. base (a background, or reverse video) goes under every segment,
// so a segment's own reset never ends a row's shading. flat drops the
// per-segment styles: a selected row in a monochrome theme is plain
// reverse video, where explicit colors would be remapped off the ramp.
type cells struct {
	b    strings.Builder
	w    int
	base string
	flat bool
}

func (c *cells) put(style, text string) {
	if c.flat {
		style = ""
	}
	c.b.WriteString(c.base + style + text + sgrReset)
	c.w += colLen(text)
}

// raw appends already-styled content of a known width (a swatch).
func (c *cells) raw(seq string, width int) {
	c.b.WriteString(seq)
	c.w += width
}

func (c *cells) pad(width int) {
	if c.w < width {
		c.put("", strings.Repeat(" ", width-c.w))
	}
}

func (c *cells) meter(frac float64, w int, accent string) {
	fill := clampInt(int(frac*float64(w)+0.5), 0, w)
	c.put(accent, strings.Repeat("━", fill))
	c.put(sgrDim, strings.Repeat("─", w-fill))
}

func (c *cells) String() string { return c.b.String() }

// at writes content at (y, x), both 1-based.
func (u *ui) at(b *strings.Builder, y, x int, content string) {
	fmt.Fprintf(b, "\x1b[%d;%dH%s", y, x, content)
}

func flush(b *strings.Builder) {
	os.Stdout.WriteString(b.String())
}

func colLen(s string) int { return utf8.RuneCountInString(s) }

func trunc(s string, n int) string {
	if n <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n-1]) + "…"
}

func spaces(n int) string { return strings.Repeat(" ", max(0, n)) }

// title upper-cases the first letter, so labels written lowercase in the
// settings tables read as sentence case on screen.
func title(s string) string {
	if s == "" {
		return s
	}
	r, n := utf8.DecodeRuneInString(s)
	return strings.ToUpper(string(r)) + s[n:]
}

// wrapText breaks s on spaces into at most maxLines lines of width w,
// ending the last with an ellipsis when text is cut.
func wrapText(s string, w, maxLines int) []string {
	if maxLines <= 1 {
		return []string{trunc(s, w)}
	}
	var lines []string
	cur := ""
	words := strings.Fields(s)
	for i, word := range words {
		if cur == "" {
			cur = word
			continue
		}
		if colLen(cur)+1+colLen(word) <= w {
			cur += " " + word
			continue
		}
		lines = append(lines, cur)
		if len(lines) == maxLines-1 {
			cur = strings.Join(words[i:], " ")
			break
		}
		cur = word
	}
	if cur != "" {
		lines = append(lines, trunc(cur, w))
	}
	return lines
}
