package main

import (
	"fmt"
	"strings"
)

// sidebarW is the category column's width, in cells.
const sidebarW = 26

// skin is how the screen is painted. A true-color theme gets shaded
// surfaces (a sidebar a step lighter than the page, a lighter step again
// for the selection) and no borders anywhere. A monochrome theme can't
// carry shades — explicit colors would be remapped through the phosphor
// ramp — so selection falls back to reverse video.
type skin struct {
	shaded bool
	accent string // fg escape, the theme's own accent
	side   string // sidebar and search field background
	sel    string // selected row background
	modal  string // overlay background
}

func (u *ui) skin() skin {
	c := &u.saved
	sk := skin{accent: truecolorFg(c.Phosphor.High)}
	if !c.TrueColor {
		return sk
	}
	bg, fg := c.Colors.DefaultBg, c.Colors.DefaultFg
	sk.shaded = true
	sk.side = truecolorBg(lerpColor(bg, fg, 0.015))
	sk.sel = truecolorBg(lerpColor(bg, fg, 0.04))
	sk.modal = truecolorBg(lerpColor(bg, fg, 0.025))
	return sk
}

// selBase is the base style of a selected row.
func (sk skin) selBase() (base string, flat bool) {
	if sk.shaded {
		return sk.sel, false
	}
	return sgrReverse + sgrBold, true
}

func (u *ui) redraw() {
	b := &strings.Builder{}
	b.WriteString("\x1b[2J")
	if u.cols < 90 || u.rows < 22 {
		u.at(b, 1, 1, sgrReverse+" window too small — resize larger (needs 90x22+) "+sgrReset)
		flush(b)
		return
	}
	u.refreshReference()
	sk := u.skin()
	previewH := u.previewHeight()
	bodyBot := u.rows - 2 - previewH

	u.drawHeader(b, sk)
	u.drawSidebar(b, sk, 2, u.rows-2)
	u.drawSearch(b, sk)
	// A monochrome skin has no shaded surface to set the overlay apart, so
	// the pane steps aside rather than showing through around it.
	if u.picker == nil || sk.shaded {
		u.drawPane(b, sk, 3, bodyBot)
	}
	if previewH > 0 && (u.picker == nil || sk.shaded) {
		u.drawPreview(b, sk, bodyBot+1)
	}
	u.drawStatus(b)
	u.drawFooter(b)
	if u.picker != nil {
		u.drawPicker(b, sk)
	}
	flush(b)
}

func (u *ui) drawHeader(b *strings.Builder, sk skin) {
	c := &cells{}
	c.put(sk.accent+sgrBold, "  tubeless")
	c.put(sgrDim, " config")
	right := ""
	if u.unsaved {
		right = "● unsaved  "
	}
	c.put("", spaces(u.cols-c.w-colLen(right)))
	c.put(sk.accent, right)
	u.at(b, 1, 1, c.String())
}

func (u *ui) drawSidebar(b *strings.Builder, sk skin, y0, y1 int) {
	if sk.shaded {
		for y := y0; y <= y1; y++ {
			u.at(b, y, 1, sk.side+spaces(sidebarW)+sgrReset)
		}
	}
	for i, cat := range u.cats {
		active := i == u.cat && u.query == ""
		u.at(b, y0+1+i, 1, u.sidebarItem(sk, cat.name, active).String())
	}
	if y1-1 > y0+len(u.cats)+1 {
		u.drawAxes(b, sk, y1-1)
	}
}

func (u *ui) sidebarItem(sk skin, name string, active bool) *cells {
	if !active {
		c := &cells{base: sk.side}
		c.put("", "  ")
		c.put(sgrDim, name)
		c.pad(sidebarW)
		return c
	}
	base, flat := sk.selBase()
	c := &cells{base: base, flat: flat}
	if flat {
		c.put("", " ")
	} else {
		c.put(sk.accent, "▎")
	}
	c.put(sgrBold, " "+name)
	c.pad(sidebarW)
	return c
}

// drawAxes shows the two independent look axes at the sidebar's foot:
// which theme (colors) and which preset (effects) are active.
func (u *ui) drawAxes(b *strings.Builder, sk skin, y int) {
	for i, kv := range [][2]string{{"theme", u.cfg.Theme}, {"preset", u.cfg.Preset}} {
		c := &cells{base: sk.side}
		c.put(sgrDim, fmt.Sprintf(" %-7s", kv[0]))
		c.put("", trunc(kv[1], sidebarW-9))
		c.pad(sidebarW)
		u.at(b, y+i, 1, c.String())
	}
}

func (u *ui) drawSearch(b *strings.Builder, sk skin) {
	w := u.cols - sidebarW - 4
	c := &cells{base: sk.side}
	c.put(sk.accent, "  / ")
	switch {
	case u.searching:
		c.put("", u.query)
		c.put(sgrReverse, " ")
	case u.query != "":
		c.put("", u.query)
	default:
		c.put(sgrDim, "Search settings")
	}
	if u.query != "" {
		right := fmt.Sprintf("%d found  ", len(u.list))
		c.put("", spaces(w-c.w-colLen(right)))
		c.put(sgrDim, right)
	}
	c.pad(w)
	u.at(b, 2, sidebarW+3, c.String())
}

func (u *ui) drawPane(b *strings.Builder, sk skin, top, bot int) {
	rx := sidebarW + 2
	rw := u.cols - rx
	if len(u.list) == 0 {
		u.at(b, top+1, rx+2, sgrDim+"No settings match \""+u.query+"\""+sgrReset)
		return
	}
	lines, selTop, selBot := u.paneLines(sk, rw)
	h := bot - top + 1
	if selTop < u.scroll {
		u.scroll = selTop
	}
	if selBot >= u.scroll+h {
		u.scroll = selBot - h + 1
	}
	u.scroll = clampInt(u.scroll, 0, max(0, len(lines)-h))
	for k := 0; k < h && u.scroll+k < len(lines); k++ {
		if l := lines[u.scroll+k]; l != "" {
			u.at(b, top+k, rx, l)
		}
	}
}

// paneLines renders every row as screen lines of width rw ("" is a blank
// line), and reports which lines the selected row spans — including its
// section heading when it is the first under one — so the caller can
// scroll it into view.
func (u *ui) paneLines(sk skin, rw int) (lines []string, selTop, selBot int) {
	headingAt, afterHeading := 0, false
	for i, r := range u.list {
		if r.kind == rowSection {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			headingAt, afterHeading = len(lines), true
			lines = append(lines, headingLine(r.section))
			continue
		}
		sel := i == u.sel
		start := len(lines)
		lines = append(lines, u.settingLines(r, sel, sk, rw)...)
		if sel {
			selTop, selBot = start, len(lines)-1
			if afterHeading {
				selTop = headingAt
			}
		}
		afterHeading = false
		if sel || !r.set.compact {
			lines = append(lines, "")
		}
	}
	return lines, selTop, selBot
}

func headingLine(section string) string {
	c := &cells{}
	c.put("", "  ")
	c.put(sgrBold, title(section))
	return c.String()
}

// settingLines is one setting: a title line with its value on the right,
// then its description beneath (wrapped when selected). Compact rows are
// the title line alone until selected.
func (u *ui) settingLines(r panelRow, sel bool, sk skin, rw int) []string {
	base, flat := "", false
	if sel {
		base, flat = sk.selBase()
	}
	newRow := func() *cells {
		c := &cells{base: base, flat: flat}
		if sel && !flat {
			c.put(sk.accent, "▎")
		} else {
			c.put("", " ")
		}
		c.put("", " ")
		return c
	}
	lines := []string{u.titleLine(r.set, sel, sk, rw, newRow())}
	if r.set.compact && !sel {
		return lines
	}
	help, maxLines := r.set.help, 1
	if r.crumb != "" {
		help = r.crumb + "  ·  " + help
	}
	if sel {
		maxLines = 4
	}
	for _, l := range wrapText(help, rw-4, maxLines) {
		c := newRow()
		c.put(sgrDim, l)
		c.pad(rw)
		lines = append(lines, c.String())
	}
	return lines
}

const meterW = 12

// titleLine fills c with a setting's label, modified dot, and — right
// aligned — its meter, swatch and value. The selected row shows arrows
// around the value (or an enter hint when it opens a picker).
func (u *ui) titleLine(s *setting, sel bool, sk skin, rw int, c *cells) string {
	val := s.get(&u.cfg)
	meterOn := sel && s.rangeOf != nil && rw >= 72
	valW := colLen(val)
	switch {
	case sel && s.pick != nil:
		valW += 7
	case sel:
		valW += 4
	}
	rightW := valW
	if meterOn {
		rightW += meterW + 2
	}
	if s.swatch != nil {
		rightW += 3
	}
	mod := u.modified(s)
	dotW := 0
	if mod {
		dotW = 2
	}
	label := trunc(title(s.label), max(3, rw-2-2-rightW-1-dotW))
	labelStyle := ""
	if sel {
		labelStyle = sgrBold
	}
	c.put(labelStyle, label)
	if mod {
		c.put(sk.accent, " ●")
	}
	c.put("", spaces(rw-2-c.w-rightW))
	if meterOn {
		v, lo, hi := s.rangeOf(&u.cfg)
		c.meter((v-lo)/max(hi-lo, 1e-9), meterW, sk.accent)
		c.put("", "  ")
	}
	if s.swatch != nil {
		c.raw(truecolorBg(s.swatch(&u.cfg))+"  "+sgrReset, 2)
		c.put("", " ")
	}
	switch {
	case sel && s.pick != nil:
		c.put(sgrBold, val)
		c.put(sgrDim, "  enter")
	case sel:
		c.put(sk.accent, "‹ ")
		c.put(sgrBold, val)
		c.put(sk.accent, " ›")
	default:
		c.put("", val)
	}
	c.pad(rw)
	return c.String()
}

func (u *ui) drawStatus(b *strings.Builder) {
	if u.status != "" {
		u.at(b, u.rows-1, sidebarW+4, sgrDim+trunc(u.status, u.cols-sidebarW-6)+sgrReset)
	}
}

func (u *ui) drawFooter(b *strings.Builder) {
	hint := " tab category · jk move · hl change · enter pick · / search · s save · r reset · q quit"
	switch {
	case u.picker != nil:
		hint = " type to filter · ↑↓ choose · enter pick · esc cancel"
	case u.searching:
		hint = " type to filter · ↑↓ move · enter keep results · esc clear"
	case u.query != "":
		hint = " jk move · hl change · / refine · esc clear search · s save · q quit"
	}
	u.at(b, u.rows, 1, sgrDim+trunc(hint, u.cols)+sgrReset)
}

// drawPicker overlays a centered list over the screen: a title, a
// search line, and the matches — shaded like the sidebar, no border.
func (u *ui) drawPicker(b *strings.Builder, sk skin) {
	p := u.picker
	w := min(u.cols-6, 56)
	h := min(u.rows-6, 18)
	if w < 24 || h < 6 {
		return
	}
	x0, y0 := (u.cols-w)/2+1, (u.rows-h)/2+1
	row := func(y int, c *cells) {
		c.pad(w)
		u.at(b, y, x0, c.String())
	}

	head := &cells{base: sk.modal}
	if !sk.shaded {
		head = &cells{base: sgrReverse + sgrBold, flat: true}
	}
	head.put(sk.accent+sgrBold, "  "+title(p.spec.title))
	row(y0, head)

	search := &cells{base: sk.modal}
	search.put(sgrDim, "  / ")
	search.put("", p.query)
	search.put(sgrReverse, " ")
	row(y0+1, search)

	u.drawPickerItems(row, sk, y0+2, h-3)

	foot := &cells{base: sk.modal}
	foot.put(sgrDim, fmt.Sprintf("  %d of %d", len(p.matches), len(p.items)))
	row(y0+h-1, foot)
}

func (u *ui) drawPickerItems(row func(int, *cells), sk skin, y0, h int) {
	p := u.picker
	if len(p.matches) == 0 {
		c := &cells{base: sk.modal}
		c.put(sgrDim, "  "+p.emptyNote())
		row(y0, c)
		for y := y0 + 1; y < y0+h; y++ {
			row(y, &cells{base: sk.modal})
		}
		return
	}
	start := 0
	if p.sel >= h {
		start = p.sel - h + 1
	}
	current := p.spec.current(&p.snapshot.cfg)
	for k := 0; k < h; k++ {
		c := &cells{base: sk.modal}
		if i := start + k; i < len(p.matches) {
			c = pickerItem(sk, p.matches[i], i == p.sel, p.matches[i] == current)
		}
		row(y0+k, c)
	}
}

func pickerItem(sk skin, name string, sel, current bool) *cells {
	c := &cells{base: sk.modal}
	if sel {
		base, flat := sk.selBase()
		c = &cells{base: base, flat: flat}
	}
	if sel && sk.shaded {
		c.put(sk.accent, "▎")
	} else {
		c.put("", " ")
	}
	c.put("", " "+name)
	if current {
		c.put(sk.accent, " ●")
	}
	return c
}

func (p *picker) emptyNote() string {
	switch {
	case p.err != "":
		return "fontconfig unavailable — type an exact name, enter to use it"
	case p.spec.free:
		return "no match — enter uses \"" + p.query + "\" as typed"
	default:
		return "no match"
	}
}
