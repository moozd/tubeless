package main

import (
	"runtime"

	"github.com/go-gl/glfw/v3.4/glfw"

	"github.com/moozd/tubeless/pkg/applet/cef"
)

// windowsKeyCodes maps GLFW keys that are not letters or digits to the
// Windows virtual key codes Chromium uses for KeyboardEvent.keyCode.
var windowsKeyCodes = map[glfw.Key]int{
	glfw.KeySpace: 0x20, glfw.KeyApostrophe: 0xDE, glfw.KeyComma: 0xBC,
	glfw.KeyMinus: 0xBD, glfw.KeyPeriod: 0xBE, glfw.KeySlash: 0xBF,
	glfw.KeySemicolon: 0xBA, glfw.KeyEqual: 0xBB,
	glfw.KeyLeftBracket: 0xDB, glfw.KeyBackslash: 0xDC,
	glfw.KeyRightBracket: 0xDD, glfw.KeyGraveAccent: 0xC0,
	glfw.KeyEscape: 0x1B, glfw.KeyEnter: 0x0D, glfw.KeyTab: 0x09,
	glfw.KeyBackspace: 0x08, glfw.KeyInsert: 0x2D, glfw.KeyDelete: 0x2E,
	glfw.KeyRight: 0x27, glfw.KeyLeft: 0x25, glfw.KeyDown: 0x28,
	glfw.KeyUp: 0x26, glfw.KeyPageUp: 0x21, glfw.KeyPageDown: 0x22,
	glfw.KeyHome: 0x24, glfw.KeyEnd: 0x23, glfw.KeyCapsLock: 0x14,
	glfw.KeyScrollLock: 0x91, glfw.KeyNumLock: 0x90,
	glfw.KeyPrintScreen: 0x2C, glfw.KeyPause: 0x13,
	glfw.KeyKPDecimal: 0x6E, glfw.KeyKPDivide: 0x6F,
	glfw.KeyKPMultiply: 0x6A, glfw.KeyKPSubtract: 0x6D,
	glfw.KeyKPAdd: 0x6B, glfw.KeyKPEnter: 0x0D, glfw.KeyKPEqual: 0xBB,
	glfw.KeyLeftShift: 0x10, glfw.KeyRightShift: 0x10,
	glfw.KeyLeftControl: 0x11, glfw.KeyRightControl: 0x11,
	glfw.KeyLeftAlt: 0x12, glfw.KeyRightAlt: 0x12,
	glfw.KeyLeftSuper: 0x5B, glfw.KeyRightSuper: 0x5C, glfw.KeyMenu: 0x5D,
}

// controlChars are the keys that type a character GLFW's char callback
// never reports. Chromium still wants a char event for them (Enter
// submits a form through it).
var controlChars = map[glfw.Key]uint16{
	glfw.KeyEnter: '\r', glfw.KeyKPEnter: '\r', glfw.KeyTab: '\t',
	glfw.KeyBackspace: 0x08, glfw.KeyEscape: 0x1B,
}

// windowsKeyCode returns the virtual key code for key, or 0.
func windowsKeyCode(key glfw.Key) int {
	switch {
	case key >= glfw.KeyA && key <= glfw.KeyZ:
		return 0x41 + int(key-glfw.KeyA)
	case key >= glfw.Key0 && key <= glfw.Key9:
		return 0x30 + int(key-glfw.Key0)
	case key >= glfw.KeyF1 && key <= glfw.KeyF24:
		return 0x70 + int(key-glfw.KeyF1)
	case key >= glfw.KeyKP0 && key <= glfw.KeyKP9:
		return 0x60 + int(key-glfw.KeyKP0)
	}
	return windowsKeyCodes[key]
}

// scancodeOffset turns a GLFW scancode into the platform key code
// Chromium derives KeyboardEvent.code from: the XKB keycode on Linux,
// the virtual key code on macOS. GLFW reports the raw evdev code on
// Wayland, which is eight lower than the XKB code; X11 and macOS
// already match. Run it after glfw.Init.
func scancodeOffset() int {
	if glfw.GetPlatform() == glfw.PlatformWayland {
		return 8
	}
	return 0
}

// cefModifiers converts GLFW modifier bits into CEF event flags.
func cefModifiers(mods glfw.ModifierKey) uint32 {
	var flags uint32
	if mods&glfw.ModShift != 0 {
		flags |= cef.FlagShift
	}
	if mods&glfw.ModControl != 0 {
		flags |= cef.FlagControl
	}
	if mods&glfw.ModAlt != 0 {
		flags |= cef.FlagAlt
	}
	if mods&glfw.ModSuper != 0 {
		flags |= cef.FlagCommand
	}
	if mods&glfw.ModCapsLock != 0 {
		flags |= cef.FlagCapsLock
	}
	if mods&glfw.ModNumLock != 0 {
		flags |= cef.FlagNumLock
	}
	return flags
}

// keyEvents turns one GLFW key callback into the CEF events Chromium
// expects: a raw key down (and, for Enter, Tab, Backspace and Escape,
// a char event) on press or repeat, a key up on release.
func keyEvents(key glfw.Key, nativeCode int, action glfw.Action, mods glfw.ModifierKey) []cef.KeyEvent {
	base := cef.KeyEvent{
		Modifiers:      cefModifiers(mods),
		WindowsKeyCode: windowsKeyCode(key),
		NativeKeyCode:  nativeCode,
	}
	if key >= glfw.KeyKP0 && key <= glfw.KeyKPEqual {
		base.Modifiers |= cef.FlagKeyPad
	}
	if action == glfw.Release {
		base.Type = cef.KeyUp
		return []cef.KeyEvent{base}
	}
	if action == glfw.Repeat {
		base.Modifiers |= cef.FlagRepeat
	}
	base.Type = cef.KeyRawDown
	events := []cef.KeyEvent{base}
	if c, ok := controlChars[key]; ok {
		char := base
		char.Type = cef.KeyChar
		char.Character, char.UnmodifiedCharacter = c, c
		events = append(events, char)
	}
	return events
}

// charEvents are the CEF events for text GLFW's char callback reports.
// Runes outside the Basic Multilingual Plane go as a surrogate pair.
func charEvents(r rune, mods glfw.ModifierKey) []cef.KeyEvent {
	units := []uint16{uint16(r)}
	if r > 0xFFFF {
		r -= 0x10000
		units = []uint16{uint16(0xD800 + r>>10), uint16(0xDC00 + r&0x3FF)}
	}
	events := make([]cef.KeyEvent, 0, len(units))
	for _, u := range units {
		events = append(events, cef.KeyEvent{
			Type:                cef.KeyChar,
			Modifiers:           cefModifiers(mods),
			WindowsKeyCode:      int(u),
			Character:           u,
			UnmodifiedCharacter: u,
		})
	}
	return events
}

// withNativeChars gives the key down and key up events the characters
// of the native key event, on macOS. Chromium there makes its own
// NSEvent from them: with none, it takes the event for a modifier
// change, and the page never sees the key. The characters also follow
// the keyboard layout, which GLFW key codes do not.
func withNativeChars(events []cef.KeyEvent) []cef.KeyEvent {
	chars, unmodified, ok := cef.CurrentKeyChars()
	if !ok {
		return events
	}
	for i := range events {
		if events[i].Type != cef.KeyChar {
			events[i].Character, events[i].UnmodifiedCharacter = chars, unmodified
		}
	}
	return events
}

// editShortcuts are the macOS edit keys. An offscreen page has no menu
// bar to turn them into edit commands, so tubeless sends the commands.
var editShortcuts = map[keyChord]cef.EditCommand{
	{glfw.KeyZ, glfw.ModSuper}:                 cef.EditUndo,
	{glfw.KeyZ, glfw.ModSuper | glfw.ModShift}: cef.EditRedo,
	{glfw.KeyX, glfw.ModSuper}:                 cef.EditCut,
	{glfw.KeyC, glfw.ModSuper}:                 cef.EditCopy,
	{glfw.KeyV, glfw.ModSuper}:                 cef.EditPaste,
	{glfw.KeyA, glfw.ModSuper}:                 cef.EditSelectAll,
}

// editCommand returns the edit command a key is the shortcut for. On
// Linux these keys reach the page as plain key events and work.
func editCommand(key glfw.Key, mods glfw.ModifierKey) (cef.EditCommand, bool) {
	if runtime.GOOS != "darwin" {
		return 0, false
	}
	cmd, ok := editShortcuts[keyChord{key, mods & chordMods}]
	return cmd, ok
}
