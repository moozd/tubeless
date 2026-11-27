package applet

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Theme is the terminal's look, handed to applets so a page uses the
// same colors and font as the cells around it. Colors are CSS hex.
type Theme struct {
	Fg        string     `json:"fg"`
	Bg        string     `json:"bg"`
	Palette   [16]string `json:"palette"`
	Cursor    string     `json:"cursor"`
	Selection Selection  `json:"selection"`
	Font      Font       `json:"font"`
	Cell      Cell       `json:"cell"`
	IsDark    bool       `json:"isDark"`
	// CSS is the theme as CSS custom properties, for the page runtime.
	CSS string `json:"css"`
}

// withCSS fills CSS from the other fields.
func (t Theme) withCSS() Theme {
	t.CSS = t.cssVars()
	return t
}

// Selection is the selected-text colors: the terminal swaps fg and bg.
type Selection struct {
	Fg string `json:"fg"`
	Bg string `json:"bg"`
}

// Font is the terminal font. Size is in CSS pixels.
type Font struct {
	Family string  `json:"family"`
	Size   float64 `json:"size"`
}

// Cell is one terminal cell in CSS pixels.
type Cell struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// cssVars is the theme as CSS custom properties on :root.
func (t Theme) cssVars() string {
	var b strings.Builder
	b.WriteString(":root{")
	fmt.Fprintf(&b, "--tb-fg:%s;--tb-bg:%s;--tb-cursor:%s;", t.Fg, t.Bg, t.Cursor)
	fmt.Fprintf(&b, "--tb-selection-fg:%s;--tb-selection-bg:%s;", t.Selection.Fg, t.Selection.Bg)
	fmt.Fprintf(&b, "--tb-font:%s;--tb-font-size:%gpx;", cssFontFamily(t.Font.Family), t.Font.Size)
	fmt.Fprintf(&b, "--tb-cell-width:%gpx;--tb-cell-height:%gpx;", t.Cell.Width, t.Cell.Height)
	for i, c := range t.Palette {
		fmt.Fprintf(&b, "--tb-ansi-%d:%s;", i, c)
	}
	scheme := "light"
	if t.IsDark {
		scheme = "dark"
	}
	fmt.Fprintf(&b, "color-scheme:%s;}", scheme)
	return b.String()
}

func cssFontFamily(family string) string {
	if family == "" {
		return "monospace"
	}
	quoted, err := json.Marshal(family)
	if err != nil {
		return "monospace"
	}
	return string(quoted) + ",monospace"
}
