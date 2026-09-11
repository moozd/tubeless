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

func wireInput(win *render.Window, sess *ptyio.Session, shared *atomic.Pointer[screen.Screen], sel *render.Selection, fontZoom chan<- int) {
	win.SetCharModsCallback(func(_ *glfw.Window, r rune, mods glfw.ModifierKey) {
		// Ctrl is handled entirely by handleSpecialKey below, which owns
		// every Ctrl combination this terminal forwards (Ctrl+A..Z, Ctrl+
		// [\]^_) — X11's key translation still fires this char callback for
		// most of them too (Ctrl held translates the keysym straight to
		// its C0 control code), so writing here as well would send every
		// Ctrl+key twice. tmux (and anything else keying off a single
		// prefix byte, e.g. Ctrl+A/Ctrl+B) breaks outright on the
		// duplicate: the prefix byte arrives, then the *next* byte is a
		// second copy of the same control code instead of the actual
		// command key.
		if mods&glfw.ModControl != 0 {
			return
		}
		// Meta/Alt sends ESC before the character — the cross-terminal
		// convention (xterm calls it metaSendsEscape) that shells/readline
		// rely on for bindings like Alt+. or Alt+d.
		if mods&glfw.ModAlt != 0 {
			sess.Write([]byte{0x1b})
		}
		sess.Write([]byte(string(r)))
	})
	win.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, _ int, action glfw.Action, mods glfw.ModifierKey) {
		if action != glfw.Press && action != glfw.Repeat {
			return
		}
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
		handleSpecialKey(sess, key, mods, shared.Load().ApplicationCursorKeys)
	})
}

// handleSpecialKey encodes any key that doesn't arrive as a character —
// arrows, editing/navigation keys, function keys, Tab/Enter/Backspace/
// Escape, and Ctrl combinations — into the byte sequence xterm (and every
// terminal compatible with it, ghostty included) would send. Without the
// modifier-aware encoding below, anything that leans on a modified key —
// Shift+Tab to reverse-cycle, Ctrl+Left/Right to jump words, Alt+Backspace
// to delete a word, Ctrl+\ to quit a hung program — silently does nothing.
func handleSpecialKey(sess *ptyio.Session, key glfw.Key, mods glfw.ModifierKey, appCursor bool) {
	if mods&glfw.ModControl != 0 {
		if key >= glfw.KeyA && key <= glfw.KeyZ {
			sess.Write([]byte{byte(key-glfw.KeyA) + 1})
			return
		}
		if b, ok := ctrlBase[key]; ok {
			sess.Write([]byte{b & 0x1f})
			return
		}
	}
	if key == glfw.KeyTab && mods&glfw.ModShift != 0 {
		sess.Write([]byte{0x1b, '[', 'Z'}) // CSI Z: back-tab
		return
	}
	if seq, ok := plainKeys[key]; ok {
		writeMeta(sess, mods, seq)
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
// to auto-indent every line of a multi-line paste. Uses GLFW's clipboard
// (cross-platform: X11/Wayland selection on Linux, NSPasteboard on macOS)
// rather than shelling out to xclip/pbpaste.
func pasteFromClipboard(win *render.Window, sess *ptyio.Session, shared *atomic.Pointer[screen.Screen]) {
	text := win.GetClipboardString()
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
