// Command tubeless-config is the in-terminal settings UI for tubeless. Run
// it as the terminal's app so it draws inside the SAME window:
//
//	tubeless --shell=$(which tubeless-config)
//
// It is a plain VT text-mode program (the same family as cmd/tektest), so
// everything on screen is real terminal content: the host's renderer paints
// box/border/block glyphs through its shape-blur layer and text stays sharp
// — the config screen doubles as the renderer's test bench. Selections and
// block bars use reverse video (bright phosphor block, black text) so they
// read clearly.
//
// Every change is written to ~/.config/tubeless/config.toml immediately;
// the running tubeless host watches that file and re-applies non-font
// settings live, rebuilding its font atlas when font/atlas fields change.
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"unicode/utf8"
	"unsafe"

	"github.com/moozd/tubeless/pkg/config"
	"github.com/moozd/tubeless/pkg/font"
)

type ui struct {
	cfg    config.Config
	path   string
	sel    int
	list   []panelRow
	cols   int
	rows   int
	status string
	esc    []byte

	// Font family picker modal (see openFontPicker) — a searchable list of
	// installed families, opened from the font.family setting instead of
	// the old free-text path editor.
	fontPicker  bool
	fontQuery   string
	fontAll     []string // every installed family, fetched once and cached
	fontErr     string   // set when fc-list failed; picker falls back to free text
	fontMatches []string
	fontSel     int
}

type rowKind uint8

const (
	rowSection rowKind = iota
	rowSetting
)

type panelRow struct {
	kind    rowKind
	section string
	set     *setting
}

type setting struct {
	key        string
	label      string
	help       string
	dec        int
	step       float64
	min        float64
	max        float64
	get        func(*config.Config) string
	applyStep  func(*config.Config, int) (needsFont bool)
	themeCycle bool
	fontFamily bool
}

func main() {
	path, err := config.DefaultPath()
	if err != nil {
		log.Fatalf("config dir: %v", err)
	}
	cfg, err := config.Load(path, "")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	cols, rows, err := termSize()
	if err != nil {
		cols, rows = 90, 30
	}

	raw, err := rawMode(os.Stdin.Fd())
	if err != nil {
		log.Fatalf("raw mode: %v", err)
	}
	defer raw.restore()

	u := &ui{cfg: cfg, path: path, cols: cols, rows: rows}
	u.buildList()

	// Alternate screen + hidden cursor so the UI owns the window and
	// restores the shell on exit.
	fmt.Fprint(os.Stdout, "\x1b[?1049h\x1b[?25l\x1b[2J")
	defer fmt.Fprint(os.Stdout, "\x1b[?25h\x1b[?1049l")

	sigWin := make(chan os.Signal, 1)
	signal.Notify(sigWin, syscall.SIGWINCH)
	defer signal.Stop(sigWin)

	keys := make(chan byte, 64)
	go readKeys(os.Stdin, keys)

	// The loop below only redraws in response to a keypress or a resize —
	// draw the initial screen once up front, or it stays blank (just the
	// 2J clear from above) until the user's first input arrives.
	u.redraw()

	for {
		select {
		case b := <-keys:
			if u.feed(b) {
				fmt.Fprint(os.Stdout, "\x1b[?25h\x1b[?1049l")
				return
			}
		case <-sigWin:
			if c, r, err := termSize(); err == nil {
				u.cols, u.rows = c, r
			}
		}
		u.redraw()
	}
}

// ---------------- input ----------------

func (u *ui) feed(b byte) (quit bool) {
	if len(u.esc) > 0 {
		// The '[' CSI introducer (0x5B) numerically falls in the "final
		// byte" range (0x40-0x7E) tested below but structurally isn't
		// one — it's always the second byte of a CSI sequence, never its
		// end. Testing for it too early there would treat every arrow key
		// (ESC [ A/B/C/D) as a 2-byte sequence that dispatches nothing,
		// discarding the buffer before the real final byte ever arrives.
		// A non-'[' second byte instead means the Escape was pressed on
		// its own — dispatch that as "cancel" and reprocess b normally.
		if len(u.esc) == 1 && b != '[' {
			u.esc = nil
			if u.fontPicker {
				u.closeFontPicker("cancelled")
			}
			return u.feed(b)
		}
		u.esc = append(u.esc, b)
		if len(u.esc) >= 3 && b >= 0x40 {
			s := u.esc
			u.esc = nil
			if len(s) == 3 && s[0] == 0x1b && s[1] == '[' {
				switch s[2] {
				case 'A':
					if u.fontPicker {
						u.moveFontSel(-1)
					} else {
						u.move(-1)
					}
				case 'B':
					if u.fontPicker {
						u.moveFontSel(1)
					} else {
						u.move(1)
					}
				case 'C':
					if !u.fontPicker {
						u.adjust(1)
					}
				case 'D':
					if !u.fontPicker {
						u.adjust(-1)
					}
				case 'H':
					if !u.fontPicker {
						u.sel = 0
						u.firstSetting()
					}
				case 'F':
					if !u.fontPicker {
						u.lastSetting()
					}
				}
			}
		}
		return false
	}
	if b == 0x1b {
		u.esc = append(u.esc, b)
		return false
	}
	if u.fontPicker {
		switch b {
		case '\r', '\n':
			u.commitFontPicker()
		case 0x7f, '\b':
			if rs := []rune(u.fontQuery); len(rs) > 0 {
				u.fontQuery = string(rs[:len(rs)-1])
			}
			u.refreshFontMatches()
		default:
			if b >= 0x20 {
				u.fontQuery += string(rune(b))
				u.refreshFontMatches()
			}
		}
		return false
	}
	switch b {
	case 'q':
		return true
	case 's':
		u.save()
	case 'r':
		u.resetPreset()
	case '\r', '\n':
		s := u.cur()
		if s != nil && s.fontFamily {
			u.openFontPicker()
		}
	case 'k':
		u.move(-1)
	case 'j':
		u.move(1)
	case 'l':
		u.adjust(1)
	case 'h':
		u.adjust(-1)
	}
	return false
}

func (u *ui) firstSetting() {
	for i, r := range u.list {
		if r.kind == rowSetting {
			u.sel = i
			return
		}
	}
}

func (u *ui) lastSetting() {
	for i := len(u.list) - 1; i >= 0; i-- {
		if u.list[i].kind == rowSetting {
			u.sel = i
			return
		}
	}
}

func (u *ui) move(d int) {
	n := len(u.list)
	for i := 0; i < n; i++ {
		u.sel = (u.sel + d + n) % n
		if u.list[u.sel].kind == rowSetting {
			return
		}
	}
}

func (u *ui) cur() *setting {
	if u.sel < len(u.list) && u.list[u.sel].kind == rowSetting {
		return u.list[u.sel].set
	}
	return nil
}

func (u *ui) adjust(d int) {
	s := u.cur()
	if s == nil {
		return
	}
	if needsFont := s.applyStep(&u.cfg, d); needsFont {
		u.status = "font/atlas changed — host rebuilding shortly"
	}
	u.dirty()
}

// openFontPicker opens the font.family search modal, fetching the system's
// installed families (via fontconfig) once and caching them for the rest
// of the session — fc-list is a subprocess call, not worth repeating on
// every open. If it fails (fontconfig not installed), the picker falls
// back to a plain search box the user can still type an exact family
// name into.
func (u *ui) openFontPicker() {
	if u.fontAll == nil && u.fontErr == "" {
		names, err := font.SystemFamilies()
		if err != nil {
			u.fontErr = err.Error()
		} else {
			u.fontAll = names
		}
	}
	u.fontPicker = true
	u.fontQuery = u.cfg.Font.Family
	u.fontSel = 0
	u.refreshFontMatches()
}

func (u *ui) closeFontPicker(status string) {
	u.fontPicker = false
	u.status = status
}

// commitFontPicker sets Font.Family to the highlighted match, or — if
// fc-list found nothing matching (or isn't available at all) — to
// whatever was typed, so an exact family name always works even without
// fontconfig enumeration.
func (u *ui) commitFontPicker() {
	family := strings.TrimSpace(u.fontQuery)
	if u.fontSel < len(u.fontMatches) {
		family = u.fontMatches[u.fontSel]
	}
	u.cfg.Font.Family = family
	u.closeFontPicker("font family changed — host rebuilding")
	u.dirty()
}

func (u *ui) moveFontSel(d int) {
	n := len(u.fontMatches)
	if n == 0 {
		return
	}
	u.fontSel = clampInt(u.fontSel+d, 0, n-1)
}

// refreshFontMatches re-filters fontAll by fontQuery (fuzzyMatch) and
// sorts by match quality, tightest first.
func (u *ui) refreshFontMatches() {
	u.fontMatches = u.fontMatches[:0]
	scores := map[string]int{}
	for _, name := range u.fontAll {
		if score, ok := fuzzyMatch(u.fontQuery, name); ok {
			u.fontMatches = append(u.fontMatches, name)
			scores[name] = score
		}
	}
	sort.SliceStable(u.fontMatches, func(i, j int) bool {
		return scores[u.fontMatches[i]] < scores[u.fontMatches[j]]
	})
	if n := len(u.fontMatches); u.fontSel >= n {
		u.fontSel = max(0, n-1)
	}
}

// fuzzyMatch is a compact fzf-style subsequence filter: query matches
// candidate if every query rune appears in candidate, in order,
// case-insensitively. The score (lower is a tighter match) rewards a
// match starting earlier in the name and spanning fewer of its runes.
func fuzzyMatch(query, candidate string) (score int, ok bool) {
	if query == "" {
		return 0, true
	}
	q := []rune(strings.ToLower(query))
	c := []rune(strings.ToLower(candidate))
	qi, start := 0, -1
	for ci := range c {
		if qi < len(q) && c[ci] == q[qi] {
			if start < 0 {
				start = ci
			}
			qi++
		}
	}
	if qi < len(q) {
		return 0, false
	}
	span := len(c) - start
	return start*100 + span, true
}

func (u *ui) save() {
	if err := config.Save(u.path, u.cfg); err != nil {
		u.status = "save failed: " + err.Error()
		return
	}
	u.status = "saved → " + u.path
}

func (u *ui) resetPreset() {
	p := config.Preset(u.cfg.Theme)
	p.Font, p.Atlas = u.cfg.Font, u.cfg.Atlas
	u.cfg = p
	u.status = "reset to " + u.cfg.Theme + " preset"
	u.dirty()
}

// dirty persists every change so the host terminal (which watches the file)
// can re-apply it live.
func (u *ui) dirty() {
	if err := config.Save(u.path, u.cfg); err != nil {
		u.status = "save failed: " + err.Error()
	}
}

// ---------------- settings model ----------------

func (u *ui) buildList() {
	u.list = nil
	section := func(name string) {
		u.list = append(u.list, panelRow{kind: rowSection, section: name})
	}
	add := func(s *setting) {
		u.list = append(u.list, panelRow{kind: rowSetting, set: s})
	}
	slider := func(key, label, help, unit string, dec int, step, min, max float64,
		get func(*config.Config) float64, put func(*config.Config, float64)) *setting {
		return &setting{
			key: key, label: label, help: help, dec: dec, step: step, min: min, max: max,
			get: func(c *config.Config) string { return trimFloat(get(c), dec) + unit },
			applyStep: func(c *config.Config, d int) bool {
				put(c, roundFloat(clampFloat(get(c)+step*float64(d), min, max), dec))
				return false
			},
		}
	}
	chanSlider := func(kind string, ch rune) *setting {
		idx := map[rune]int{'r': 0, 'g': 1, 'b': 2}[ch]
		key := kind + "." + string(ch)
		return slider(key, key, "phosphor ramp channel", "", 2, 0.01, 0, 1,
			func(c *config.Config) float64 {
				if kind == "low" {
					return float64(c.Phosphor.Low[idx])
				}
				return float64(c.Phosphor.High[idx])
			},
			func(c *config.Config, v float64) {
				if kind == "low" {
					c.Phosphor.Low[idx] = float32(v)
				} else {
					c.Phosphor.High[idx] = float32(v)
				}
			})
	}

	section("theme")
	add(&setting{
		key: "theme", label: "preset", help: "cycles the rosepine/green/amber theme preset",
		themeCycle: true,
		get:        func(c *config.Config) string { return c.Theme },
		applyStep: func(c *config.Config, _ int) bool {
			next := map[string]string{"rosepine": "green", "green": "amber"}[c.Theme]
			if next == "" {
				next = "rosepine"
			}
			p := config.Preset(next)
			c.Theme, c.Phosphor, c.TrueColor, c.Colors = next, p.Phosphor, p.TrueColor, p.Colors
			return false
		},
	})

	for _, kind := range []string{"low", "high"} {
		section("phosphor — " + kind)
		for _, ch := range "rgb" {
			add(chanSlider(kind, ch))
		}
	}

	section("blur — boxes & borders")
	add(slider("blur.radius", "radius", "gaussian spread in px on blocks & box glyphs", "px", 1, 0.1, 0, 8,
		func(c *config.Config) float64 { return float64(c.Blur.Radius) },
		func(c *config.Config, v float64) { c.Blur.Radius = float32(v) }))
	add(slider("blur.strength", "strength", "how strongly the blur replaces the sharp shapes", "", 2, 0.01, 0, 1,
		func(c *config.Config) float64 { return float64(c.Blur.Strength) },
		func(c *config.Config, v float64) { c.Blur.Strength = float32(v) }))

	section("face — tube")
	add(slider("face.bg_tint", "bg tint", "brightness of the unlit screen", "", 3, 0.005, 0, 0.5,
		func(c *config.Config) float64 { return float64(c.Face.BgTint) },
		func(c *config.Config, v float64) { c.Face.BgTint = float32(v) }))
	add(slider("face.inset_shadow", "inset shadow", "radial falloff to the corners", "", 2, 0.01, 0, 1,
		func(c *config.Config) float64 { return float64(c.Face.InsetShadow) },
		func(c *config.Config, v float64) { c.Face.InsetShadow = float32(v) }))

	section("cursor")
	add(slider("cursor.glow", "glow", "halo width of the block cursor", "px", 1, 0.1, 0, 8,
		func(c *config.Config) float64 { return float64(c.Cursor.Glow) },
		func(c *config.Config, v float64) { c.Cursor.Glow = float32(v) }))
	add(slider("cursor.pulse_period", "pulse period", "breathing period of the cursor", "s", 2, 0.05, 0.2, 4,
		func(c *config.Config) float64 { return float64(c.Cursor.PulsePeriod) },
		func(c *config.Config, v float64) { c.Cursor.PulsePeriod = float32(v) }))

	section("contrast")
	add(slider("contrast.min_delta", "min contrast", "minimum fg/bg gap on the mono ramp", "", 2, 0.01, 0, 1,
		func(c *config.Config) float64 { return float64(c.Contrast.MinDelta) },
		func(c *config.Config, v float64) { c.Contrast.MinDelta = float32(v) }))

	section("font")
	add(&setting{
		key: "font.size", label: "size", help: "logical pixel height (rebuilds the atlas)",
		get: func(c *config.Config) string { return fmt.Sprintf("%d px", c.Font.Size) },
		applyStep: func(c *config.Config, d int) bool {
			c.Font.Size = clampInt(c.Font.Size+2*d, 10, 96)
			return true
		},
	})
	add(&setting{
		key: "atlas.scale", label: "atlas scale", help: "glyph supersampling (rebuilds the atlas)",
		get: func(c *config.Config) string { return fmt.Sprintf("%d×", c.Atlas.Scale) },
		applyStep: func(c *config.Config, d int) bool {
			c.Atlas.Scale = clampInt(c.Atlas.Scale+d, 1, 8)
			return true
		},
	})
	add(&setting{
		key: "font.gamma", label: "gamma", help: "coverage curve shaping (rebuilds the atlas)", dec: 2, step: 0.05, min: 0.5, max: 2,
		get: func(c *config.Config) string { return trimFloat(c.Atlas.Gamma, 2) },
		applyStep: func(c *config.Config, d int) bool {
			c.Atlas.Gamma = roundFloat(clampFloat(c.Atlas.Gamma+0.05*float64(d), 0.5, 2), 2)
			return true
		},
	})
	add(&setting{
		key: "font.family", label: "family", help: "installed font family; Enter to search", fontFamily: true,
		get: func(c *config.Config) string {
			if c.Font.Family == "" {
				return "(bundled FiraCode Nerd)"
			}
			return c.Font.Family
		},
	})

	u.firstSetting()
}

func trimFloat(v float64, dec int) string {
	s := fmt.Sprintf("%.*f", dec, v)
	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}

func roundFloat(v float64, dec int) float64 {
	p := 1
	for range dec {
		p *= 10
	}
	f := float64(p)
	return float64(int(v*f+0.5)) / f
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// SGR helpers.
const (
	sgrReset   = "\x1b[0m"
	sgrBold    = "\x1b[1m"
	sgrDim     = "\x1b[2m"
	sgrReverse = "\x1b[7m"
	sgrBg256   = "\x1b[48;5;"
)

// The UI is plain terminal content: box-drawing borders, block meters,
// reverse-video selection rows and background-colored regions. The host
// paints all of it through the same pipeline as the shell, so borders and
// blocks come from its blurred shape layer while text stays sharp — the
// config screen doubles as a live test bench for those effects.

func (u *ui) redraw() {
	b := &strings.Builder{}
	b.WriteString("\x1b[2J")

	if u.cols < 50 || u.rows < 14 {
		u.revRow(b, 1, " window too small — resize larger ")
		flush(b)
		return
	}
	cols, rows := u.cols, u.rows
	s := u.cur()

	// Header bar.
	head := "TUBELESS CONFIG"
	if s != nil {
		head += "  ·  " + s.label
	}
	if u.fontPicker {
		head += "  ·  searching fonts…"
	}
	u.revRow(b, 1, trunc(head, cols))

	// Focus line: selected setting = value, live block meter, short help.
	core := ""
	if s != nil {
		name := padRight(s.label, 18)
		core = "  " + name + " = " + s.get(&u.cfg)
		if v, ok := knob(&u.cfg, s.key); ok {
			frac := 0.0
			if v.max > v.min {
				frac = (v.v - v.min) / (v.max - v.min)
			}
			mw := 14
			if cols < 70 {
				mw = 6
			}
			core += "   " + meterStr(frac, mw)
		}
		if help := trunc(s.help, max(0, cols-colLen(core)-2)); colLen(help) > 0 {
			core += "  " + sgrDim + help + sgrReset
		}
	}
	if u.status != "" {
		core += "  " + sgrDim + u.status + sgrReset
	}
	u.row(b, 2, core)

	// Panels: settings box on top, preview box at the bottom.
	previewH := 0
	if rows >= 24 {
		previewH = 7
	}
	listBottom := rows - previewH - 1
	if listBottom < 5 {
		listBottom = 5
	}
	u.drawSettings(b, 3, listBottom)
	if previewH > 0 && listBottom+6 <= rows-1 {
		u.drawPreview(b, listBottom+1, listBottom+6)
	}

	// Footer bar.
	u.revRow(b, rows, " ↑↓/jk select · ←→/hl adjust · s save · r reset · q quit ")

	if u.fontPicker {
		u.drawFontPicker(b)
	}
	flush(b)
}

// drawFontPicker overlays a centered modal on top of the normal screen (it
// draws last, after everything else in redraw): a search box, then either
// the matching family list or — when fc-list wasn't available — a hint
// that Enter uses whatever was typed literally.
func (u *ui) drawFontPicker(b *strings.Builder) {
	cols, rows := u.cols, u.rows
	w := min(cols-4, 60)
	h := min(rows-4, 20)
	if w < 20 || h < 6 {
		return
	}
	x0 := (cols - w) / 2
	y0 := (rows - h) / 2

	line := func(y int, s string) { fmt.Fprintf(b, "\x1b[%d;%dH%s", y, x0+1, s) }

	line(y0, boxTop("font family", w))
	line(y0+1, boxText("search: "+u.fontQuery+"_", w))
	line(y0+2, "│"+strings.Repeat("─", w-2)+"│")

	listY0, listY1 := y0+3, y0+h-2
	switch {
	case u.fontErr != "":
		line(listY0, boxText(trunc("fontconfig unavailable: "+u.fontErr, w-4), w))
		line(listY0+1, boxText("type an exact family name, Enter to use it", w))
		for y := listY0 + 2; y <= listY1; y++ {
			line(y, boxText("", w))
		}
	case len(u.fontMatches) == 0:
		line(listY0, boxText("no match — Enter uses \""+u.fontQuery+"\" as typed", w))
		for y := listY0 + 1; y <= listY1; y++ {
			line(y, boxText("", w))
		}
	default:
		innerW := w - 2
		capRows := listY1 - listY0 + 1
		start := 0
		if len(u.fontMatches) > capRows && u.fontSel >= capRows {
			start = u.fontSel - capRows + 1
		}
		for y := listY0; y <= listY1; y++ {
			i := start + (y - listY0)
			content := colPad(strings.Repeat(" ", innerW), innerW)
			if i < len(u.fontMatches) {
				content = colPad(" "+trunc(u.fontMatches[i], innerW-1), innerW)
			}
			if i == u.fontSel && i < len(u.fontMatches) {
				line(y, "│"+sgrReverse+content+sgrReset+"│")
			} else {
				line(y, "│"+content+"│")
			}
		}
	}
	line(y0+h-1, boxBottom(w))
	line(y0+h, trunc(" ↑↓ select · Enter pick · Esc cancel ", cols))
}

// drawSettings renders the list inside a boxed panel; the selected row is
// a full-width reverse block (bright phosphor bar, black text).
func (u *ui) drawSettings(b *strings.Builder, y0, y1 int) {
	cols := u.cols
	innerW := cols - 2
	u.row(b, y0, boxTop("settings", cols))
	u.row(b, y1, boxBottom(cols))

	innerY0, innerY1 := y0+1, y1-1
	capRows := innerY1 - innerY0 + 1
	n := len(u.list)
	start := 0
	if n > capRows && u.sel >= capRows {
		start = u.sel - capRows + 1
	}

	for y := innerY0; y <= innerY1; y++ {
		i := start + (y - innerY0)
		sel := i < n && i == u.sel
		plain := strings.Repeat(" ", innerW)
		isSection := false
		if i < n {
			r := u.list[i]
			switch r.kind {
			case rowSection:
				isSection = true
				plain = "  " + trunc(strings.ToUpper(r.section), innerW-2)
			case rowSetting:
				value := r.set.get(&u.cfg)
				ll, vl := colLen(r.set.label), colLen(value)
				label := trunc(r.set.label, max(0, innerW-vl-3))
				ll = colLen(label)
				gap := innerW - 2 - ll - vl
				if sel {
					plain = " " + label + strings.Repeat(" ", max(0, gap)) + value
				} else {
					plain = "  " + label + strings.Repeat(".", max(1, gap)) + value
				}
			}
		}
		plain = colPad(plain, innerW)
		switch {
		case sel:
			u.row(b, y, "│"+sgrReverse+plain+sgrReset+"│")
		case isSection:
			u.row(b, y, "│"+sgrDim+plain+sgrReset+"│")
		default:
			u.row(b, y, "│"+plain+"│")
		}
	}
}

// drawPreview is a small visual test bench: an inverted block bar, a rounded
// box with crisp text, border-weight samples, a block meter and a luminance
// ramp — all exercising the host's shape blur.
func (u *ui) drawPreview(b *strings.Builder, y0, y1 int) {
	cols := u.cols
	innerW := cols - 4
	if innerW < 20 {
		return
	}
	u.row(b, y0, boxTop("preview · effects test", cols))

	// Inverted block bar: a solid colored band the host rounds/blurs.
	band := colPad(" block band (inverted) ", min(innerW, 30))
	u.row(b, y0+1, "  "+sgrReverse+band+sgrReset)

	// Rounded box with text inside: crisp glyphs, smooth borders.
	bw := min(innerW-2, 46)
	u.row(b, y0+2, "  "+boxTopNoTitle(bw))
	u.row(b, y0+3, "  "+boxText(" text stays sharp · borders stay smooth ", bw))
	u.row(b, y0+4, "  "+boxBottomNoTitle(bw))

	// Border weights + a meter + a luminance ramp.
	u.row(b, y0+5, "  ─ │   ━ ┃   ═ ║   ╭ ╮ ╰ ╯    "+meterStr(0.65, 12)+"   "+rampStr(14))

	u.row(b, y1, boxBottom(cols))
}

// row emits one line at y (1-based). The screen is cleared with 2J at the
// top of every redraw, so no per-line erase is needed — and an ESC[K right
// after a full-width line would wipe its last cell.
func (u *ui) row(b *strings.Builder, y int, content string) {
	fmt.Fprintf(b, "\x1b[%d;1H%s", y, content)
}

// revRow emits a full-width reverse bar.
func (u *ui) revRow(b *strings.Builder, y int, s string) {
	u.row(b, y, sgrReverse+colPad(trunc(s, u.cols), u.cols)+sgrReset)
}

// meterStr renders a horizontal block bar for frac in [0,1].
func meterStr(frac float64, w int) string {
	if w < 1 {
		return ""
	}
	fill := clampInt(int(frac*float64(w)), 0, w)
	out := sgrBold + strings.Repeat("█", fill) + sgrReset
	if rem := w - fill; rem > 0 {
		out += sgrDim + strings.Repeat("░", rem) + sgrReset
	}
	return out
}

// rampStr returns w background blocks stepping a luminance ramp.
func rampStr(w int) string {
	if w < 2 {
		return ""
	}
	var b strings.Builder
	for i := 0; i < w; i++ {
		n := 232 + i*(24/(w-1))
		if n > 255 {
			n = 255
		}
		fmt.Fprintf(&b, "%s%dm ", sgrBg256, n)
	}
	b.WriteString(sgrReset)
	return b.String()
}

// boxTop/boxBottom draw a rounded single-line box spanning `cols` columns,
// with an optional title after the top-left corner.
func boxTop(title string, cols int) string {
	if cols < 4 {
		return "╭╮"
	}
	body := "─"
	if title != "" {
		body = "─ " + title + " "
	}
	// body occupies the interior; top = corner + interior(width cols-2) + corner.
	inner := trunc(body, cols-2)
	if n := colLen(inner); n < cols-2 {
		inner += strings.Repeat("─", cols-2-n)
	}
	return "╭" + inner + "╮"
}

func boxBottom(cols int) string {
	if cols < 2 {
		return "╰╯"
	}
	return "╰" + strings.Repeat("─", cols-2) + "╯"
}

// boxTopNoTitle/boxBottomNoTitle are the same without a title, for nested
// boxes of width w.
func boxTopNoTitle(w int) string {
	return "╭" + strings.Repeat("─", max(0, w-2)) + "╮"
}

func boxBottomNoTitle(w int) string {
	return "╰" + strings.Repeat("─", max(0, w-2)) + "╯"
}

// boxText returns a bordered line with text centred in a w-wide box.
func boxText(t string, w int) string {
	if w < 4 {
		return "││"
	}
	t = trunc(t, w-4)
	tl := colLen(t)
	side := (w - 4 - tl + 1) / 2
	// "│ " (2) + side + t (tl) + trailing + " │" (2) must total exactly w,
	// matching boxTopNoTitle/boxBottomNoTitle's border rows — trailing pad
	// is w-4-side-tl, not w-3-side-tl (that extra column made this row one
	// character wider than the box's top/bottom, pushing its right border
	// one column past theirs).
	return "│ " + strings.Repeat(" ", side) + t + strings.Repeat(" ", w-4-side-tl) + " │"
}

func padTo(s string, w int) string { return colPad(s, w) }

func flush(b *strings.Builder) {
	os.Stdout.WriteString(b.String())
}

func padRight(s string, w int) string {
	return colPad(trunc(s, w), w)
}

func colLen(s string) int { return utf8.RuneCountInString(s) }

// colPad pads s with spaces until it occupies exactly w columns (runes).
func colPad(s string, w int) string {
	n := colLen(s)
	if n >= w {
		return s
	}
	return s + strings.Repeat(" ", w-n)
}

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

// knob returns the numeric value + range for a slider key.
func knob(c *config.Config, key string) (struct {
	v, min, max float64
}, bool) {
	type rng struct{ v, min, max float64 }
	if key == "low.r" || key == "low.g" || key == "low.b" {
		idx := map[string]int{"r": 0, "g": 1, "b": 2}[key[4:]]
		return rng{float64(c.Phosphor.Low[idx]), 0, 1}, true
	}
	if key == "high.r" || key == "high.g" || key == "high.b" {
		idx := map[string]int{"r": 0, "g": 1, "b": 2}[key[5:]]
		return rng{float64(c.Phosphor.High[idx]), 0, 1}, true
	}
	switch key {
	case "blur.radius":
		return rng{float64(c.Blur.Radius), 0, 8}, true
	case "blur.strength":
		return rng{float64(c.Blur.Strength), 0, 1}, true
	case "face.bg_tint":
		return rng{float64(c.Face.BgTint), 0, 0.5}, true
	case "face.inset_shadow":
		return rng{float64(c.Face.InsetShadow), 0, 1}, true
	case "cursor.glow":
		return rng{float64(c.Cursor.Glow), 0, 8}, true
	case "cursor.pulse_period":
		return rng{float64(c.Cursor.PulsePeriod), 0.2, 4}, true
	case "contrast.min_delta":
		return rng{float64(c.Contrast.MinDelta), 0, 1}, true
	case "font.size":
		return rng{float64(c.Font.Size), 10, 96}, true
	case "atlas.scale":
		return rng{float64(c.Atlas.Scale), 1, 8}, true
	case "font.gamma":
		return rng{float64(c.Atlas.Gamma), 0.5, 2}, true
	}
	return struct {
		v, min, max float64
	}{}, false
}

// ---------------- terminal plumbing ----------------

type winsize struct{ Row, Col, Xpixel, Ypixel uint16 }

// termSize reports the PTY size in cells.
func termSize() (int, int, error) {
	var ws winsize
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, os.Stdout.Fd(),
		uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)))
	if errno != 0 {
		return 0, 0, errno
	}
	return int(ws.Col), int(ws.Row), nil
}

// rawTermios holds the terminal state needed to switch stdin to raw mode.
type rawTermios struct {
	fd   uintptr
	term syscall.Termios
}

// rawMode puts stdin into cbreak raw mode (no echo, no line buffering) and
// returns a handle whose restore() puts it back.
func rawMode(fd uintptr) (*rawTermios, error) {
	var term syscall.Termios
	if _, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, fd,
		uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&term)), 0, 0, 0); errno != 0 {
		return nil, errno
	}
	raw := term
	raw.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.IEXTEN | syscall.ISIG
	raw.Iflag &^= syscall.IXON | syscall.ICRNL | syscall.BRKINT | syscall.INPCK | syscall.ISTRIP
	raw.Oflag &^= syscall.OPOST
	raw.Cflag |= syscall.CS8
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if _, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, fd,
		uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&raw)), 0, 0, 0); errno != 0 {
		return nil, errno
	}
	return &rawTermios{fd: fd, term: term}, nil
}

func (r *rawTermios) restore() {
	syscall.Syscall6(syscall.SYS_IOCTL, r.fd,
		uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&r.term)), 0, 0, 0)
}

// readKeys forwards every input byte to out until EOF.
func readKeys(f *os.File, out chan<- byte) {
	buf := make([]byte, 256)
	for {
		n, err := f.Read(buf)
		for i := 0; i < n; i++ {
			out <- buf[i]
		}
		if err != nil {
			return
		}
	}
}
