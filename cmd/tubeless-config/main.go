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
// Edits only take effect in memory as you navigate — nothing is written to
// ~/.config/tubeless/config.toml until you press 's'. The running tubeless
// host watches that file and re-applies non-font settings live once saved,
// rebuilding its font atlas when font/atlas fields change.
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"slices"
	"sort"
	"strings"
	"syscall"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/moozd/tubeless/pkg/config"
	"github.com/moozd/tubeless/pkg/font"
)

type ui struct {
	cfg     config.Config
	path    string
	sel     int
	list    []panelRow
	cols    int
	rows    int
	status  string
	esc     []byte
	unsaved bool

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
		u.status = "font/atlas changed — press s to save"
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
	u.closeFontPicker("font family changed — press s to save")
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
	u.unsaved = false
	u.status = "saved → " + u.path
}

func (u *ui) resetPreset() {
	p := config.Preset(u.cfg.Theme)
	p.Font, p.Atlas = u.cfg.Font, u.cfg.Atlas
	u.cfg = p
	u.status = "reset to " + u.cfg.Theme + " preset"
	u.dirty()
}

// dirty marks u.cfg as having an unsaved edit. It deliberately does not
// write to disk — the host terminal only re-applies config.toml on its own
// watch cycle, so writing here would mean every arrow-key nudge takes
// effect immediately in the running terminal; changes only take effect
// once the user explicitly presses 's' (see save()).
func (u *ui) dirty() {
	u.unsaved = true
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
		key: "theme", label: "preset", help: "cycles through every theme preset",
		themeCycle: true,
		get:        func(c *config.Config) string { return c.Theme },
		applyStep: func(c *config.Config, d int) bool {
			names := config.PresetNames()
			idx := slices.Index(names, c.Theme)
			if idx < 0 {
				idx = 0
			} else {
				n := len(names)
				idx = ((idx+d)%n + n) % n
			}
			next := names[idx]
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
				return "FiraCode Nerd Font"
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

// accent is the one color that ties every pane together: the theme's own
// CRT-chrome accent (Phosphor.High — see pkg/config's doc comment on
// Phosphor), set for every theme, monochrome or TrueColor alike, so pane
// headers/borders always paint in a color that actually belongs to the
// active theme instead of a fixed one.
func (u *ui) accent() [3]float32 { return u.cfg.Phosphor.High }

func (u *ui) redraw() {
	b := &strings.Builder{}
	b.WriteString("\x1b[2J")

	if u.cols < 90 || u.rows < 22 {
		u.revRow(b, 1, " window too small — resize larger (needs 90x22+) ")
		flush(b)
		return
	}
	cols, rows := u.cols, u.rows
	accent := u.accent()

	// Top bar: app name + unsaved flag only — the selected setting's own
	// detail now lives entirely in the SETTING pane below, not duplicated
	// up here.
	head := " TUBELESS CONFIG"
	if u.unsaved {
		head += "  ·  unsaved changes (s to save)"
	}
	if u.fontPicker {
		head += "  ·  searching fonts…"
	}
	u.at(b, 1, 1, truecolorBg(accent)+contrastFg(accent)+sgrBold+colPad(trunc(head, cols), cols)+sgrReset)

	// Grid: SETTINGS spans the full left column; SETTING/SWATCHES/PREVIEW
	// stack in the right column.
	contentTop, contentBottom := 2, rows-1
	contentH := contentBottom - contentTop + 1
	leftW := clampInt(cols*42/100, 34, 50)
	rightX := leftW + 1
	rightW := cols - leftW

	settingH, swatchesH := 5, 5
	previewH := contentH - settingH - swatchesH
	ySetting0 := contentTop
	ySwatches0 := ySetting0 + settingH
	yPreview0 := ySwatches0 + swatchesH

	u.drawSettings(b, 1, contentTop, leftW, contentBottom, accent)
	u.drawSetting(b, rightX, ySetting0, rightW, settingH, accent)
	u.drawSwatches(b, rightX, ySwatches0, rightW, swatchesH, accent)
	if previewH >= 6 {
		u.drawPreview(b, rightX, yPreview0, rightW, contentBottom, accent)
	}

	// Footer bar.
	u.revRow(b, rows, " ↑↓/jk select · ←→/hl adjust · s save · r reset · q quit ")

	if u.fontPicker {
		u.drawFontPicker(b)
	}
	flush(b)
}

// at writes content at (y, x), both 1-based — the positioned equivalent
// of row(), for panes that don't start at column 1.
func (u *ui) at(b *strings.Builder, y, x int, content string) {
	fmt.Fprintf(b, "\x1b[%d;%dH%s", y, x, content)
}

// styledLine composes one pane row's interior content while tracking its
// true on-screen column width alongside the ANSI-laden string — plain()
// measures via colLen, chunk() trusts a caller-supplied width for
// already-colored fixed-width fragments (meterStr/rampStr/swatch blocks,
// whose own escape codes colLen can't see through). Building rows this way is what lets
// drawSetting/drawSwatches/drawPreview embed real color inside a
// bordered pane without corrupting the border alignment math trunc/
// colPad alone would get wrong on a string that already contains escape
// sequences.
type styledLine struct {
	buf strings.Builder
	w   int
}

func (s *styledLine) plain(text string) {
	s.buf.WriteString(text)
	s.w += colLen(text)
}

func (s *styledLine) chunk(text string, width int) {
	s.buf.WriteString(text)
	s.w += width
}

// paneTitle draws a solid accent-colored title bar spanning w columns —
// every pane's flat "colorful solid box" header.
func (u *ui) paneTitle(b *strings.Builder, y, x0, w int, title string, accent [3]float32) {
	bar := colPad(" "+strings.ToUpper(title), w)
	u.at(b, y, x0, truecolorBg(accent)+contrastFg(accent)+sgrBold+bar+sgrReset)
}

// paneBottom draws a pane's accent-tinted rounded bottom border.
func (u *ui) paneBottom(b *strings.Builder, y, x0, w int, accent [3]float32) {
	u.at(b, y, x0, truecolorFg(accent)+boxBottomNoTitle(w)+sgrReset)
}

// paneRow draws one bordered content row from plain text: content is
// colPad/trunc'd to the pane's interior width first (so it must not
// already contain ANSI codes — see paneRowRaw for that case), then
// optionally wrapped in style (a dim/bold/color SGR prefix) or, when
// selected, painted as a solid accent bar (the settings list's
// highlighted row).
func (u *ui) paneRow(b *strings.Builder, y, x0, w int, content, style string, accent [3]float32, selected bool) {
	innerW := w - 2
	line := colPad(trunc(content, innerW), innerW)
	switch {
	case selected:
		line = truecolorBg(accent) + contrastFg(accent) + sgrBold + line + sgrReset
	case style != "":
		line = style + line + sgrReset
	}
	edge := truecolorFg(accent)
	u.at(b, y, x0, edge+"│"+sgrReset+line+edge+"│"+sgrReset)
}

// paneRowRaw draws one bordered content row from a string that already
// contains its own ANSI styling (a styledLine.buf) — visibleWidth is the
// caller-tracked on-screen width (styledLine.w), since colLen can't
// measure through embedded escape codes.
func (u *ui) paneRowRaw(b *strings.Builder, y, x0, w int, content string, visibleWidth int, accent [3]float32) {
	innerW := w - 2
	pad := max(0, innerW-visibleWidth)
	edge := truecolorFg(accent)
	u.at(b, y, x0, edge+"│"+sgrReset+content+strings.Repeat(" ", pad)+edge+"│"+sgrReset)
}

// drawFontPicker overlays a centered modal on top of the normal screen (it
// draws last, after everything else in redraw): a search box, then either
// the matching family list or — when fc-list wasn't available — a hint
// that Enter uses whatever was typed literally. Tinted with the theme's
// accent, matching every other pane, for visual consistency.
func (u *ui) drawFontPicker(b *strings.Builder) {
	cols, rows := u.cols, u.rows
	w := min(cols-4, 60)
	h := min(rows-4, 20)
	if w < 20 || h < 6 {
		return
	}
	x0 := (cols - w) / 2
	y0 := (rows - h) / 2
	accent := u.accent()

	line := func(y int, s string) {
		fmt.Fprintf(b, "\x1b[%d;%dH%s%s%s", y, x0+1, truecolorFg(accent), s, sgrReset)
	}

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

// drawSettings renders the section+setting list inside its own pane — the
// primary nav surface, spanning the full left column. The selected row
// paints as a solid accent bar (the theme's own color, not a fixed
// reverse-video invert); section headers dim in the same accent.
func (u *ui) drawSettings(b *strings.Builder, x0, y0, w, y1 int, accent [3]float32) {
	innerW := w - 2
	u.paneTitle(b, y0, x0, w, "settings", accent)
	u.paneBottom(b, y1, x0, w, accent)

	innerY0, innerY1 := y0+1, y1-1
	capRows := innerY1 - innerY0 + 1
	n := len(u.list)
	start := 0
	if n > capRows && u.sel >= capRows {
		start = u.sel - capRows + 1
	}
	dimAccent := truecolorFg(accent) + sgrDim

	for y := innerY0; y <= innerY1; y++ {
		i := start + (y - innerY0)
		sel := i < n && i == u.sel
		plain, style := "", ""
		if i < n {
			r := u.list[i]
			switch r.kind {
			case rowSection:
				plain = "  " + strings.ToUpper(r.section)
				style = dimAccent
			case rowSetting:
				value := r.set.get(&u.cfg)
				vl := colLen(value)
				label := trunc(r.set.label, max(0, innerW-vl-3))
				ll := colLen(label)
				gap := innerW - 2 - ll - vl
				if sel {
					plain = " " + label + strings.Repeat(" ", max(0, gap)) + value
				} else {
					plain = "  " + label + strings.Repeat(".", max(1, gap)) + value
				}
			}
		}
		u.paneRow(b, y, x0, w, plain, style, accent, sel)
	}
}

// drawSetting is the focused-setting detail pane: label = value, a live
// block meter, and the short help text (or the status line, once one is
// set) — this is the old full-width "focus line" promoted into its own
// pane, and its only home now.
func (u *ui) drawSetting(b *strings.Builder, x0, y0, w, h int, accent [3]float32) {
	u.paneTitle(b, y0, x0, w, "setting", accent)
	u.paneBottom(b, y0+h-1, x0, w, accent)
	innerW := w - 2

	s := u.cur()
	if s == nil {
		for y := y0 + 1; y < y0+h-1; y++ {
			u.paneRow(b, y, x0, w, "", "", accent, false)
		}
		return
	}
	u.paneRow(b, y0+1, x0, w, " "+s.label+" = "+s.get(&u.cfg), sgrBold, accent, false)

	var meterLine styledLine
	meterLine.plain(" ")
	if v, ok := knob(&u.cfg, s.key); ok {
		frac := 0.0
		if v.max > v.min {
			frac = (v.v - v.min) / (v.max - v.min)
		}
		mw := min(max(innerW-4, 4), 28)
		meterLine.chunk(meterStr(frac, mw), mw)
	}
	u.paneRowRaw(b, y0+2, x0, w, meterLine.buf.String(), meterLine.w, accent)

	help := s.help
	if u.status != "" {
		help = u.status
	}
	u.paneRow(b, y0+3, x0, w, " "+help, sgrDim, accent, false)
}

// drawSwatches shows the active theme's real colors as solid blocks —
// the pane that makes "colorful solid boxes" literal, using the theme's
// own RGB rather than a fixed 256-color approximation. A TrueColor theme
// gets its full 16-color palette plus default fg/bg; a monochrome theme
// (which leaves Colors unset — see pkg/config's amber/green presets) gets
// its phosphor Low→High ramp instead, since Palette would just be black.
func (u *ui) drawSwatches(b *strings.Builder, x0, y0, w, h int, accent [3]float32) {
	u.paneTitle(b, y0, x0, w, "swatches", accent)
	u.paneBottom(b, y0+h-1, x0, w, accent)
	innerW := w - 2

	if !u.cfg.TrueColor {
		u.drawRampSwatch(b, x0, y0+1, w, accent)
		return
	}

	blockRow := func(y int, colors [][3]float32) {
		var sl styledLine
		sl.plain(" ")
		for _, c := range colors {
			if sl.w+3 > innerW {
				break
			}
			sl.chunk(truecolorBg(c)+"  "+sgrReset, 2)
			sl.plain(" ")
		}
		u.paneRowRaw(b, y, x0, w, sl.buf.String(), sl.w, accent)
	}
	blockRow(y0+1, u.cfg.Colors.Palette[0:8])
	blockRow(y0+2, u.cfg.Colors.Palette[8:16])

	var fgbg styledLine
	fgbg.plain(" ")
	fgbg.chunk(truecolorBg(u.cfg.Colors.DefaultFg)+"  "+sgrReset, 2)
	fgbg.plain(" fg    ")
	fgbg.chunk(truecolorBg(u.cfg.Colors.DefaultBg)+"  "+sgrReset, 2)
	fgbg.plain(" bg")
	u.paneRowRaw(b, y0+3, x0, w, fgbg.buf.String(), fgbg.w, accent)
}

// drawRampSwatch renders the monochrome phosphor Low→High ramp as a wide
// gradient bar, for themes with no real per-cell color to swatch.
func (u *ui) drawRampSwatch(b *strings.Builder, x0, y0, w int, accent [3]float32) {
	innerW := w - 2
	steps := max(4, innerW-2)
	var sl styledLine
	sl.plain(" ")
	for i := 0; i < steps; i++ {
		t := float32(i) / float32(steps-1)
		c := lerpColor(u.cfg.Phosphor.Low, u.cfg.Phosphor.High, t)
		sl.chunk(truecolorBg(c)+" "+sgrReset, 1)
	}
	u.paneRowRaw(b, y0, x0, w, sl.buf.String(), sl.w, accent)
	u.paneRow(b, y0+1, x0, w, " low → high phosphor ramp", sgrDim, accent, false)
}

// drawPreview is a small visual test bench: an inverted block bar, a rounded
// box with crisp text, border-weight samples, a block meter and a luminance
// ramp — all exercising the host's shape blur, inside its own pane.
func (u *ui) drawPreview(b *strings.Builder, x0, y0, w, y1 int, accent [3]float32) {
	innerW := w - 2
	u.paneTitle(b, y0, x0, w, "preview · effects test", accent)
	u.paneBottom(b, y1, x0, w, accent)

	// Inverted block bar: a solid colored band the host rounds/blurs.
	band := colPad(" block band (inverted) ", min(innerW-2, 30))
	var bandLine styledLine
	bandLine.plain(" ")
	bandLine.chunk(sgrReverse+band+sgrReset, colLen(band))
	u.paneRowRaw(b, y0+1, x0, w, bandLine.buf.String(), bandLine.w, accent)

	// Nested rounded box with crisp text — boxTopNoTitle/boxText/
	// boxBottomNoTitle are plain (no embedded ANSI), so these are safe
	// through the normal colPad/trunc path.
	bw := min(innerW-2, 46)
	u.paneRow(b, y0+2, x0, w, " "+boxTopNoTitle(bw), "", accent, false)
	u.paneRow(b, y0+3, x0, w, " "+boxText(" text stays sharp · borders stay smooth ", bw), "", accent, false)
	u.paneRow(b, y0+4, x0, w, " "+boxBottomNoTitle(bw), "", accent, false)

	// Border weights + a meter + a luminance ramp.
	weights := "─ │   ━ ┃   ═ ║   ╭ ╮ ╰ ╯    "
	mw, rw := 10, 12
	var sample styledLine
	sample.plain(" ")
	sample.plain(weights)
	sample.chunk(meterStr(0.65, mw), mw)
	sample.plain("   ")
	sample.chunk(rampStr(rw), rw)
	u.paneRowRaw(b, y0+5, x0, w, sample.buf.String(), sample.w, accent)

	for y := y0 + 6; y < y1; y++ {
		u.paneRow(b, y, x0, w, "", "", accent, false)
	}
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

func flush(b *strings.Builder) {
	os.Stdout.WriteString(b.String())
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
//
// Raw-mode entry/exit and terminal size both go through golang.org/x/term
// rather than hand-rolled ioctl syscalls: the previous implementation used
// Linux's TCGETS/TCSETS ioctl requests and syscall.Termios directly, which
// don't exist under those names on macOS/BSD (they use TIOCGETA/TIOCSETA
// with a differently-laid-out termios struct) — x/term abstracts that
// per-platform difference so this file needs no GOOS-specific variant.

// termSize reports the PTY size in cells.
func termSize() (int, int, error) {
	return term.GetSize(int(os.Stdout.Fd()))
}

// rawTermios holds the terminal state needed to switch stdin to raw mode.
type rawTermios struct {
	fd    int
	state *term.State
}

// rawMode puts stdin into cbreak raw mode (no echo, no line buffering) and
// returns a handle whose restore() puts it back.
func rawMode(fd uintptr) (*rawTermios, error) {
	ifd := int(fd)
	state, err := term.MakeRaw(ifd)
	if err != nil {
		return nil, err
	}
	return &rawTermios{fd: ifd, state: state}, nil
}

func (r *rawTermios) restore() {
	term.Restore(r.fd, r.state)
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
