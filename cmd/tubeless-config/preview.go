package main

import (
	"fmt"
	"strings"
)

// previewHeight is how many lines the Theme category's live color
// preview takes under the settings; zero elsewhere or when the window
// is short.
func (u *ui) previewHeight() int {
	if u.query != "" || u.cats[u.cat].name != "Theme" || u.rows < 30 {
		return 0
	}
	if u.rows >= 46 {
		return 13
	}
	return 5
}

func (u *ui) drawPreview(b *strings.Builder, sk skin, y0 int) {
	x := sidebarW + 4
	if !u.cfg.TrueColor {
		u.drawRampPreview(b, x, y0+1)
		u.drawSampleLine(b, x, y0+3, u.cfg.Phosphor.High, u.cfg.Phosphor.Low)
	} else {
		u.drawRoleSwatches(b, x, y0)
		u.drawPaletteSwatches(b, x, y0+1)
		u.drawSampleLine(b, x, y0+3, u.cfg.Colors.DefaultFg, u.cfg.Colors.DefaultBg)
	}
	if u.previewHeight() > 5 {
		u.drawPalette256Reference(b, x, y0+5)
	}
}

// hex3 formats a linear-light color as an "#RRGGBB" sRGB hex string.
func hex3(c [3]float32) string {
	return fmt.Sprintf("#%02X%02X%02X", srgbByte(c[0]), srgbByte(c[1]), srgbByte(c[2]))
}

func (u *ui) drawRoleSwatches(b *strings.Builder, x, y int) {
	roles := []struct {
		name string
		col  [3]float32
	}{
		{"text", u.cfg.Colors.DefaultFg}, {"bg", u.cfg.Colors.DefaultBg},
		{"accent", u.cfg.Phosphor.High}, {"glow", u.cfg.Phosphor.Low},
	}
	c := &cells{}
	for _, r := range roles {
		c.put(sgrDim, r.name+" ")
		c.raw(truecolorBg(r.col)+"  "+sgrReset, 2)
		c.put("", " "+hex3(r.col)+"   ")
	}
	u.at(b, y, x, c.String())
}

func (u *ui) drawPaletteSwatches(b *strings.Builder, x, y int) {
	c := &cells{}
	for _, col := range u.cfg.Colors.Palette {
		c.raw(truecolorBg(col)+"   "+sgrReset, 3)
	}
	u.at(b, y, x, c.String())
}

// drawRampPreview renders the monochrome phosphor Low→High ramp as a
// gradient bar, for themes with no real per-cell color to swatch.
func (u *ui) drawRampPreview(b *strings.Builder, x, y int) {
	steps := min(48, u.cols-x-2)
	c := &cells{}
	for i := range steps {
		t := float32(i) / float32(max(steps-1, 1))
		c.raw(truecolorBg(lerpColor(u.cfg.Phosphor.Low, u.cfg.Phosphor.High, t))+" "+sgrReset, 1)
	}
	u.at(b, y, x, c.String())
}

// drawSampleLine renders one line of sample prompt text in fg-on-bg plus
// a trailing block cursor — what a shell line looks like in this theme.
func (u *ui) drawSampleLine(b *strings.Builder, x, y int, fg, bg [3]float32) {
	const sample = " tubeless ❯ echo hello, world"
	line := truecolorBg(bg) + truecolorFg(fg) + colPad(sample, 48) + sgrReset
	u.at(b, y, x, line+sgrReverse+" "+sgrReset)
}

func colPad(s string, w int) string {
	if n := colLen(s); n < w {
		return s + spaces(w-n)
	}
	return s
}

// drawPalette256Reference renders the fixed xterm 256-color table's
// upper range (16-255 — the 6x6x6 cube and grayscale ramp that never
// change per-theme) as a read-only swatch grid, for confirming what an
// app's indexed colourN styling actually looks like.
func (u *ui) drawPalette256Reference(b *strings.Builder, x, y int) {
	u.at(b, y, x, sgrDim+"256-color reference (16-255, fixed)"+sgrReset)
	// One row per R-level keeps the 216-color cube a legible 36-cell
	// G x B square per row instead of one 216-wide strip.
	for r := range 6 {
		c := &cells{}
		for g := range 6 {
			for bl := range 6 {
				c.raw(ansi256Bg(16+36*r+6*g+bl)+" "+sgrReset, 1)
			}
		}
		u.at(b, y+1+r, x, c.String())
	}
	gray := &cells{}
	for idx := 232; idx <= 255; idx++ {
		gray.raw(ansi256Bg(idx)+"  "+sgrReset, 2)
	}
	u.at(b, y+7, x, gray.String())
}
