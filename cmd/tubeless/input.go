package main

import (
	"fmt"
	"runtime"
	"sync/atomic"

	"github.com/go-gl/glfw/v3.4/glfw"

	"github.com/moozd/tubeless/pkg/ptyio"
	"github.com/moozd/tubeless/pkg/render"
	"github.com/moozd/tubeless/pkg/screen"
)

// plainKeys are keys whose base byte sequence never changes with mods —
// Ctrl doesn't apply to any of them, and Alt just ESC-prefixes the byte
// like it does for any ordinary character (see writeMeta).
var plainKeys = map[glfw.Key][]byte{
	glfw.KeyEnter:     {'\r'},
	glfw.KeyBackspace: {0x7f},
	glfw.KeyEscape:    {0x1b},
	glfw.KeyTab:       {'\t'},
}

// ctrlBase maps a GLFW key to the ASCII character Ctrl+<key> is defined
// against: an xterm C0 control code is just that character's low 5 bits
// (char & 0x1f) — the same rule that makes Ctrl+A..Z map to 1..26, here
// extended to the handful of punctuation keys sharing row with them.
// Ctrl+\ in particular matters beyond typing: it's the tty's default
// QUIT character, so forwarding it is what lets Ctrl+\ kill a hung
// foreground program the way it does in any other terminal.
var ctrlBase = map[glfw.Key]byte{
	glfw.KeySpace:        '@',
	glfw.KeyLeftBracket:  '[',
	glfw.KeyBackslash:    '\\',
	glfw.KeyRightBracket: ']',
	glfw.Key6:            '^',
	glfw.KeyMinus:        '_',
}

// cursorKey is an arrow key or Home/End: CSI <letter> normally, SS3
// <letter> in DECCKM (application cursor keys) mode when unmodified, and
// CSI 1;<mod><letter> whenever a modifier is held — DECCKM only ever
// swaps the unmodified form (xterm's ctlseqs), so Ctrl/Shift/Alt+arrow
// keep working the same regardless of what mode the app has requested.
var cursorKey = map[glfw.Key]byte{
	glfw.KeyUp:    'A',
	glfw.KeyDown:  'B',
	glfw.KeyRight: 'C',
	glfw.KeyLeft:  'D',
	glfw.KeyHome:  'H',
	glfw.KeyEnd:   'F',
}

// fnKey is F1-F4: SS3 <letter> unmodified, CSI 1;<mod><letter> modified.
var fnKey = map[glfw.Key]byte{
	glfw.KeyF1: 'P',
	glfw.KeyF2: 'Q',
	glfw.KeyF3: 'R',
	glfw.KeyF4: 'S',
}

// tildeKey is a "CSI <n>[;<mod>]~" key: Insert/Delete/PageUp/PageDown and
// the F5-F12 row, none of which have a letter final byte of their own.
var tildeKey = map[glfw.Key]int{
	glfw.KeyInsert:   2,
	glfw.KeyDelete:   3,
	glfw.KeyPageUp:   5,
	glfw.KeyPageDown: 6,
	glfw.KeyF5:       15,
	glfw.KeyF6:       17,
	glfw.KeyF7:       18,
	glfw.KeyF8:       19,
	glfw.KeyF9:       20,
	glfw.KeyF10:      21,
	glfw.KeyF11:      23,
	glfw.KeyF12:      24,
}

// keyboardMode is the terminal's current keyboard-reporting state, read
// from the latest published Screen and used to decide how to encode a key.
type keyboardMode struct {
	modifyOtherKeys int // xterm "CSI > 4;m" level (tmux extended-keys)
	kittyFlags      int // kitty keyboard protocol flag bitmask
}

func keyboardModeOf(scr *screen.Screen) keyboardMode {
	return keyboardMode{modifyOtherKeys: scr.ModifyOtherKeys, kittyFlags: scr.KittyFlags()}
}

func wireInput(win *render.Window, sess *ptyio.Session, shared *atomic.Pointer[screen.Screen], sel *render.Selection, fontZoom chan<- int) {
	win.SetCharModsCallback(func(_ *glfw.Window, r rune, mods glfw.ModifierKey) {
		handleChar(sess, r, mods, keyboardModeOf(shared.Load()))
	})
	win.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, _ int, action glfw.Action, mods glfw.ModifierKey) {
		scr := shared.Load()
		mode := keyboardModeOf(scr)
		if action == glfw.Press {
			if isPasteShortcut(key, mods) {
				pasteFromClipboard(win, sess, shared)
				return
			}
			if isCopyShortcut(key, mods) {
				copySelectionToClipboard(win, shared, *sel)
				return
			}
			if d, ok := fontZoomDelta(key, mods); ok {
				select {
				case fontZoom <- d:
				default:
				}
				return
			}
		}
		if action != glfw.Press && action != glfw.Repeat && action != glfw.Release {
			return
		}
		// Release events only matter when the kitty protocol has asked for
		// event-type reporting; without it a release is indistinguishable
		// from a press and forwarding it would double every keystroke.
		if action == glfw.Release && mode.kittyFlags&screen.KittyReportEvents == 0 {
			return
		}
		handleSpecialKey(sess, key, action, mods, scr.ApplicationCursorKeys, mode)
	})
}

// handleChar emits a text-producing key. It stays silent when the kitty
// protocol's report-all-keys flag is on (the key callback then owns every
// key event, text keys included, so writing here would double-emit), and
// for Ctrl/Alt-modified keys that handleSpecialKey encodes as CSI u
// sequences instead.
func handleChar(sess *ptyio.Session, r rune, mods glfw.ModifierKey, mode keyboardMode) {
	if mode.kittyFlags&screen.KittyReportAllKeys != 0 {
		return
	}
	// Ctrl is owned entirely by handleSpecialKey — X11's key translation
	// still fires this char callback for most Ctrl combinations (Ctrl held
	// translates the keysym straight to its C0 control code), so writing
	// here as well would send every Ctrl+key twice. tmux (and anything
	// keying off a single prefix byte, e.g. Ctrl+A/B) breaks outright on
	// the duplicate.
	if mods&glfw.ModControl != 0 {
		return
	}
	// Under kitty disambiguation, Alt (and Shift+Alt) is encoded by the key
	// callback as a CSI u sequence rather than the legacy ESC prefix.
	if mode.kittyFlags&screen.KittyDisambiguate != 0 && mods&glfw.ModAlt != 0 {
		return
	}
	// Meta/Alt sends ESC before the character — the cross-terminal
	// convention (xterm calls it metaSendsEscape) that shells/readline
	// rely on for bindings like Alt+. or Alt+d.
	if mods&glfw.ModAlt != 0 {
		sess.Write([]byte{0x1b})
	}
	sess.Write([]byte(string(r)))
}

// handleSpecialKey encodes any key that doesn't arrive as a character —
// arrows, editing/navigation keys, function keys, Tab/Enter/Backspace/
// Escape, and Ctrl combinations — into the byte sequence xterm (and every
// terminal compatible with it, ghostty included) would send. Without the
// modifier-aware encoding below, anything that leans on a modified key —
// Shift+Tab to reverse-cycle, Ctrl+Left/Right to jump words, Alt+Backspace
// to delete a word, Ctrl+\ to quit a hung program — silently does nothing.
//
// When an app has enabled extended key reporting (kitty keyboard protocol
// flags, or tmux's modifyOtherKeys request), the affected keys are instead
// encoded as kitty "CSI u" sequences so modifiers survive the trip — see
// extendedFor and emitExtendedKey.
func handleSpecialKey(sess *ptyio.Session, key glfw.Key, action glfw.Action, mods glfw.ModifierKey, appCursor bool, mode keyboardMode) {
	// Legacy encodings have no way to carry an event type, so a release is
	// only meaningful for keys being reported in CSI u form.
	if action == glfw.Release && !extendedFor(key, mods, mode) {
		return
	}

	if mods&glfw.ModControl != 0 {
		if key >= glfw.KeyA && key <= glfw.KeyZ {
			base := rune('a' + (key - glfw.KeyA))
			if extendedFor(key, mods, mode) {
				emitExtendedKey(sess, int(base), base, mods, action, mode.kittyFlags)
			} else {
				sess.Write([]byte{byte(key-glfw.KeyA) + 1})
			}
			return
		}
		if b, ok := ctrlBase[key]; ok {
			if extendedFor(key, mods, mode) {
				base, _ := baseRune(key)
				emitExtendedKey(sess, int(base), base, mods, action, mode.kittyFlags)
			} else {
				sess.Write([]byte{b & 0x1f})
			}
			return
		}
	}
	if key == glfw.KeyTab && mods&glfw.ModShift != 0 && !extendedFor(key, mods, mode) {
		sess.Write([]byte{0x1b, '[', 'Z'}) // CSI Z: back-tab
		return
	}
	if seq, ok := plainKeys[key]; ok {
		if extendedFor(key, mods, mode) {
			emitExtendedKey(sess, int(seq[0]), 0, mods, action, mode.kittyFlags)
		} else {
			writeMeta(sess, mods, seq)
		}
		return
	}
	if final, ok := cursorKey[key]; ok {
		sess.Write(encodeCursorKey(final, mods, appCursor))
		return
	}
	if final, ok := fnKey[key]; ok {
		sess.Write(encodeFnKey(final, mods))
		return
	}
	if n, ok := tildeKey[key]; ok {
		sess.Write(encodeTildeKey(n, mods))
		return
	}
	if base, ok := baseRune(key); ok {
		if extendedFor(key, mods, mode) {
			emitExtendedKey(sess, int(base), base, mods, action, mode.kittyFlags)
		}
	}
}

// isCopyShortcut reports whether key+mods is this platform's terminal
// copy binding: Cmd+C on macOS (Cmd is never claimed by a shell control
// code, so it's free for the OS-level convention), Ctrl+Shift+C
// elsewhere (plain Ctrl+C must stay SIGINT).
func isCopyShortcut(key glfw.Key, mods glfw.ModifierKey) bool {
	if key != glfw.KeyC {
		return false
	}
	if runtime.GOOS == "darwin" {
		return mods&glfw.ModSuper != 0
	}
	return mods&glfw.ModControl != 0 && mods&glfw.ModShift != 0
}

// isPasteShortcut reports whether key+mods is this platform's terminal
// paste binding: Shift+Insert everywhere (the conventional cross-DE
// binding), plus Cmd+V on macOS or Ctrl+Shift+V elsewhere — Ctrl+V alone
// is left to whatever the shell's own line editing does with it, same as
// every other terminal.
func isPasteShortcut(key glfw.Key, mods glfw.ModifierKey) bool {
	if key == glfw.KeyInsert && mods&glfw.ModShift != 0 {
		return true
	}
	if key != glfw.KeyV {
		return false
	}
	if runtime.GOOS == "darwin" {
		return mods&glfw.ModSuper != 0
	}
	return mods&glfw.ModControl != 0 && mods&glfw.ModShift != 0
}

// fontZoomDelta reports whether key+mods is the live font-zoom shortcut —
// Ctrl+=/Ctrl+- on Linux/Windows, Cmd+=/Cmd+- on macOS, the same
// modifier convention isCopyShortcut/isPasteShortcut use to pick Cmd vs
// Ctrl per platform. The unshifted '=' key doubles as '+' on a US
// layout, so Ctrl+= alone (no Shift needed) is the zoom-in binding, same
// as every browser's Ctrl/Cmd+Plus.
func fontZoomDelta(key glfw.Key, mods glfw.ModifierKey) (delta int, ok bool) {
	active := mods&glfw.ModControl != 0
	if runtime.GOOS == "darwin" {
		active = mods&glfw.ModSuper != 0
	}
	if !active {
		return 0, false
	}
	switch key {
	case glfw.KeyEqual, glfw.KeyKPAdd:
		return 1, true
	case glfw.KeyMinus, glfw.KeyKPSubtract:
		return -1, true
	}
	return 0, false
}

// writeMeta ESC-prefixes seq when Alt is held — the same metaSendsEscape
// convention as the char callback, for the keys (Tab/Enter/Backspace/
// Escape) that don't have their own modifier-encoded CSI form.
func writeMeta(sess *ptyio.Session, mods glfw.ModifierKey, seq []byte) {
	if mods&glfw.ModAlt != 0 {
		sess.Write([]byte{0x1b})
	}
	sess.Write(seq)
}

// csiMod converts GLFW's modifier bitmask into xterm's CSI modifier
// parameter (1 = none, else 1 + shift(1) + alt(2) + ctrl(4) + super(8))
// and reports whether any modifier is actually held — an unmodified key
// omits ";<mod>" entirely rather than sending the redundant ";1".
func csiMod(mods glfw.ModifierKey) (n int, has bool) {
	n = 1
	if mods&glfw.ModShift != 0 {
		n += 1
	}
	if mods&glfw.ModAlt != 0 {
		n += 2
	}
	if mods&glfw.ModControl != 0 {
		n += 4
	}
	if mods&glfw.ModSuper != 0 {
		n += 8
	}
	return n, n != 1
}

// hasMods reports whether any modifier that affects a key's meaning is
// held (Shift/Alt/Ctrl/Super) — the set a key event's encoding has to care
// about, as opposed to locks (Caps/Num) which GLFW also reports.
func hasMods(mods glfw.ModifierKey) bool {
	return mods&(glfw.ModShift|glfw.ModAlt|glfw.ModControl|glfw.ModSuper) != 0
}

// extendedFor reports whether key+mods should be encoded as a kitty "CSI u"
// sequence under mode, rather than its legacy sequence. This is the single
// source of truth for both choosing the encoding and deciding whether a
// release event is representable (legacy forms can't carry an event type).
func extendedFor(key glfw.Key, mods glfw.ModifierKey, mode keyboardMode) bool {
	reportAll := mode.kittyFlags&screen.KittyReportAllKeys != 0
	disambiguate := mode.kittyFlags&screen.KittyDisambiguate != 0

	if _, ok := plainKeys[key]; ok {
		if reportAll {
			return true
		}
		// kitty's disambiguate flag leaves Enter/Tab/Backspace legacy but
		// does disambiguate Escape; modifyOtherKeys reports any modified key.
		if key == glfw.KeyEscape && disambiguate {
			return true
		}
		if mode.modifyOtherKeys >= 2 {
			return hasMods(mods)
		}
		if mode.modifyOtherKeys == 1 {
			return mods&(glfw.ModControl|glfw.ModAlt) != 0
		}
		return false
	}

	if mods&glfw.ModControl != 0 {
		if key >= glfw.KeyA && key <= glfw.KeyZ {
			return reportAll || disambiguate
		}
		if _, ok := ctrlBase[key]; ok {
			return reportAll || disambiguate
		}
	}

	if _, ok := baseRune(key); ok {
		return reportAll || (disambiguate && mods&(glfw.ModAlt|glfw.ModControl) != 0)
	}
	return false
}

// baseRune maps a GLFW key to its unshifted US-layout character — the
// "code point" the kitty protocol uses as a text key's code — reporting
// false for keys that produce no character.
func baseRune(key glfw.Key) (rune, bool) {
	switch {
	case key >= glfw.KeyA && key <= glfw.KeyZ:
		return 'a' + rune(key-glfw.KeyA), true
	case key >= glfw.Key0 && key <= glfw.Key9:
		return '0' + rune(key-glfw.Key0), true
	}
	switch key {
	case glfw.KeySpace:
		return ' ', true
	case glfw.KeyApostrophe:
		return '\'', true
	case glfw.KeyComma:
		return ',', true
	case glfw.KeyMinus:
		return '-', true
	case glfw.KeyPeriod:
		return '.', true
	case glfw.KeySlash:
		return '/', true
	case glfw.KeySemicolon:
		return ';', true
	case glfw.KeyEqual:
		return '=', true
	case glfw.KeyLeftBracket:
		return '[', true
	case glfw.KeyBackslash:
		return '\\', true
	case glfw.KeyRightBracket:
		return ']', true
	case glfw.KeyGraveAccent:
		return '`', true
	}
	return 0, false
}

// shiftedRunes is the US-layout shifted form of the punctuation keys that
// don't simply uppercase; letters are handled inline in shiftRune.
var shiftedRunes = map[rune]rune{
	'1': '!', '2': '@', '3': '#', '4': '$', '5': '%',
	'6': '^', '7': '&', '8': '*', '9': '(', '0': ')',
	'`': '~', '-': '_', '=': '+', '[': '{', ']': '}',
	'\\': '|', ';': ':', '\'': '"', ',': '<', '.': '>', '/': '?',
}

func shiftRune(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - 'a' + 'A'
	}
	if s, ok := shiftedRunes[r]; ok {
		return s
	}
	return r
}

func eventType(action glfw.Action) int {
	switch action {
	case glfw.Repeat:
		return 2
	case glfw.Release:
		return 3
	default:
		return 1
	}
}

// emitExtendedKey writes a key event in the kitty protocol's "CSI u" form,
// honoring the active flags: alternate keys (shifted + base layout), event
// type (repeat/release), and associated text. base is the unshifted
// US-layout rune for text keys (0 for pure functional keys), code its key
// code (the Unicode codepoint for text keys, or the C0/PUA code otherwise).
func emitExtendedKey(sess *ptyio.Session, code int, base rune, mods glfw.ModifierKey, action glfw.Action, flags int) {
	sess.Write(encodeExtendedKey(code, base, mods, action, flags))
}

func encodeExtendedKey(code int, base rune, mods glfw.ModifierKey, action glfw.Action, flags int) []byte {
	b := []byte{0x1b, '['}
	b = appendDecimal(b, code)

	// Alternate keys: "code:shifted:base" (the shifted sub-field empty when
	// Shift isn't held). Only for text keys, only when requested.
	if flags&screen.KittyReportAlternate != 0 && base != 0 {
		b = append(b, ':')
		if mods&glfw.ModShift != 0 {
			b = appendDecimal(b, int(shiftRune(base)))
		}
		b = append(b, ':')
		b = appendDecimal(b, int(base))
	}

	modVal, hasMod := csiMod(mods)
	needEvent := flags&screen.KittyReportEvents != 0 && action != glfw.Press
	// Associated text only for unshifted/shifted printable keys — Ctrl/Alt
	// produce control/alt characters, not the text codepoint, and the spec
	// forbids control codes here.
	hasText := flags&screen.KittyReportAssociated != 0 && base != 0 && mods&(glfw.ModControl|glfw.ModAlt) == 0

	if hasMod || needEvent || hasText {
		b = append(b, ';')
		if hasMod {
			b = appendDecimal(b, modVal)
		} else if needEvent {
			b = appendDecimal(b, 1) // no modifiers = 1
		}
		if needEvent {
			b = append(b, ':')
			b = appendDecimal(b, eventType(action))
		}
		if hasText {
			b = append(b, ';')
			if mods&glfw.ModShift != 0 {
				b = appendDecimal(b, int(shiftRune(base)))
			} else {
				b = appendDecimal(b, int(base))
			}
		}
	}

	return append(b, 'u')
}

func appendDecimal(b []byte, n int) []byte {
	return fmt.Appendf(b, "%d", n)
}

func encodeCursorKey(final byte, mods glfw.ModifierKey, appCursor bool) []byte {
	if n, has := csiMod(mods); has {
		return fmt.Appendf(nil, "\x1b[1;%d%c", n, final)
	}
	if appCursor {
		return []byte{0x1b, 'O', final}
	}
	return []byte{0x1b, '[', final}
}

func encodeFnKey(final byte, mods glfw.ModifierKey) []byte {
	if n, has := csiMod(mods); has {
		return fmt.Appendf(nil, "\x1b[1;%d%c", n, final)
	}
	return []byte{0x1b, 'O', final}
}

func encodeTildeKey(num int, mods glfw.ModifierKey) []byte {
	if n, has := csiMod(mods); has {
		return fmt.Appendf(nil, "\x1b[%d;%d~", num, n)
	}
	return fmt.Appendf(nil, "\x1b[%d~", num)
}

// pasteFromClipboard writes the system clipboard's text into the PTY,
// wrapped in bracketed-paste markers (CSI 200~ ... CSI 201~) when the
// app has asked for that mode (CSI ?2004h — see Screen.BracketedPaste) so
// it can tell pasted text apart from typed input instead of, say, trying
// to auto-indent every line of a multi-line paste. Read via readClipboard
// so the Wayland build still sees the X11 CLIPBOARD that xclip writes.
func pasteFromClipboard(win *render.Window, sess *ptyio.Session, shared *atomic.Pointer[screen.Screen]) {
	text := readClipboard(win)
	if text == "" {
		return
	}
	if shared.Load().BracketedPaste {
		sess.Write([]byte("\x1b[200~"))
		sess.Write([]byte(text))
		sess.Write([]byte("\x1b[201~"))
		return
	}
	sess.Write([]byte(text))
}
