package main

import (
	"testing"

	"github.com/go-gl/glfw/v3.4/glfw"

	"github.com/moozd/tubeless/pkg/applet/cef"
)

func TestParseTmuxKey(t *testing.T) {
	cases := map[string]keyChord{
		"C-a":     {key: glfw.KeyA, mods: glfw.ModControl},
		"C-b":     {key: glfw.KeyB, mods: glfw.ModControl},
		"M-Space": {key: glfw.KeySpace, mods: glfw.ModAlt},
		"`":       {key: glfw.KeyGraveAccent},
		"F12":     {key: glfw.KeyUnknown},
	}
	for name, want := range cases {
		if got := parseTmuxKey(name); got != want {
			t.Errorf("parseTmuxKey(%q) = %+v, want %+v", name, got, want)
		}
	}
}

func TestPrefixSendsItselfAndNextKeyToTerminal(t *testing.T) {
	h := &appletHost{applets: map[int]*openApplet{}}
	h.input.prefix = keyChord{key: glfw.KeyA, mods: glfw.ModControl}
	if !h.takePrefix(glfw.KeyA, glfw.ModControl) {
		t.Fatal("prefix stayed with the applet")
	}
	if !h.takePrefix(glfw.KeyLeftShift, glfw.ModShift) {
		t.Fatal("a modifier after the prefix went to the applet")
	}
	if !h.takePrefix(glfw.KeyO, 0) {
		t.Fatal("key after the prefix went to the applet")
	}
	if !h.input.charToTerminal {
		t.Fatal("char of the key after the prefix will go to the applet")
	}
	if h.takePrefix(glfw.KeyX, 0) {
		t.Fatal("second key after the prefix went to the terminal")
	}
}

func TestKeyEventsForEnterAddChar(t *testing.T) {
	got := keyEvents(glfw.KeyEnter, 28, glfw.Press, 0)
	if len(got) != 2 || got[0].Type != cef.KeyRawDown || got[1].Type != cef.KeyChar || got[1].Character != '\r' {
		t.Fatalf("Enter = %+v", got)
	}
	if got[0].WindowsKeyCode != 0x0D {
		t.Fatalf("Enter key code = %#x", got[0].WindowsKeyCode)
	}
	up := keyEvents(glfw.KeyA, 30, glfw.Release, glfw.ModControl)
	if len(up) != 1 || up[0].Type != cef.KeyUp || up[0].WindowsKeyCode != 0x41 || up[0].Modifiers&cef.FlagControl == 0 {
		t.Fatalf("Ctrl+A release = %+v", up)
	}
}

func TestCharEventsSplitSurrogates(t *testing.T) {
	got := charEvents('😀', 0)
	if len(got) != 2 || got[0].Character != 0xD83D || got[1].Character != 0xDE00 {
		t.Fatalf("charEvents(U+1F600) = %+v", got)
	}
}
