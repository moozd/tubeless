package main

import (
	"testing"

	"github.com/go-gl/glfw/v3.4/glfw"

	"github.com/moozd/tubeless/pkg/screen"
)

func TestEncodeExtendedKey(t *testing.T) {
	cases := []struct {
		name   string
		code   int
		base   rune
		mods   glfw.ModifierKey
		action glfw.Action
		flags  int
		want   string
	}{
		{
			name: "shift+enter report all keys",
			code: 13, mods: glfw.ModShift, action: glfw.Press,
			flags: screen.KittyReportAllKeys,
			want:  "\x1b[13;2u",
		},
		{
			name: "shift+enter modifyOtherKeys (no kitty flags)",
			code: 13, mods: glfw.ModShift, action: glfw.Press,
			flags: 0,
			want:  "\x1b[13;2u",
		},
		{
			name: "ctrl+a disambiguate",
			code: 97, base: 'a', mods: glfw.ModControl, action: glfw.Press,
			flags: screen.KittyDisambiguate,
			want:  "\x1b[97;5u",
		},
		{
			name: "plain a report all keys",
			code: 97, base: 'a', action: glfw.Press,
			flags: screen.KittyReportAllKeys,
			want:  "\x1b[97u",
		},
		{
			name: "shift+a with alternate and associated text",
			code: 97, base: 'a', mods: glfw.ModShift, action: glfw.Press,
			flags: screen.KittyReportAllKeys | screen.KittyReportAlternate | screen.KittyReportAssociated,
			want:  "\x1b[97:65:97;2;65u",
		},
		{
			name: "release with event type",
			code: 97, base: 'a', action: glfw.Release,
			flags: screen.KittyReportAllKeys | screen.KittyReportEvents,
			want:  "\x1b[97;1:3u",
		},
		{
			name: "repeat with event type and modifier",
			code: 13, mods: glfw.ModShift, action: glfw.Repeat,
			flags: screen.KittyReportAllKeys | screen.KittyReportEvents,
			want:  "\x1b[13;2:2u",
		},
		{
			name: "alt+a disambiguate",
			code: 97, base: 'a', mods: glfw.ModAlt, action: glfw.Press,
			flags: screen.KittyDisambiguate,
			want:  "\x1b[97;3u",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := string(encodeExtendedKey(c.code, c.base, c.mods, c.action, c.flags)); got != c.want {
				t.Errorf("encodeExtendedKey = %q, want %q", got, c.want)
			}
		})
	}
}

func TestExtendedFor(t *testing.T) {
	cases := []struct {
		name string
		key  glfw.Key
		mods glfw.ModifierKey
		mode keyboardMode
		want bool
	}{
		{"plain enter legacy", glfw.KeyEnter, 0, keyboardMode{}, false},
		{"shift+enter modifyOtherKeys2", glfw.KeyEnter, glfw.ModShift, keyboardMode{modifyOtherKeys: 2}, true},
		{"plain enter modifyOtherKeys2", glfw.KeyEnter, 0, keyboardMode{modifyOtherKeys: 2}, false},
		{"ctrl+enter modifyOtherKeys2", glfw.KeyEnter, glfw.ModControl, keyboardMode{modifyOtherKeys: 2}, true},
		{"shift+enter report all", glfw.KeyEnter, glfw.ModShift, keyboardMode{kittyFlags: screen.KittyReportAllKeys}, true},
		{"enter disambiguate stays legacy", glfw.KeyEnter, glfw.ModShift, keyboardMode{kittyFlags: screen.KittyDisambiguate}, false},
		{"escape disambiguate", glfw.KeyEscape, 0, keyboardMode{kittyFlags: screen.KittyDisambiguate}, true},
		{"ctrl+a legacy", glfw.KeyA, glfw.ModControl, keyboardMode{}, false},
		{"ctrl+a disambiguate", glfw.KeyA, glfw.ModControl, keyboardMode{kittyFlags: screen.KittyDisambiguate}, true},
		{"plain a legacy", glfw.KeyA, 0, keyboardMode{}, false},
		{"plain a report all", glfw.KeyA, 0, keyboardMode{kittyFlags: screen.KittyReportAllKeys}, true},
		{"alt+a disambiguate", glfw.KeyA, glfw.ModAlt, keyboardMode{kittyFlags: screen.KittyDisambiguate}, true},
		{"shift+a disambiguate stays text", glfw.KeyA, glfw.ModShift, keyboardMode{kittyFlags: screen.KittyDisambiguate}, false},
		{"arrow legacy", glfw.KeyUp, 0, keyboardMode{}, false},
		{"arrow report all still legacy form", glfw.KeyUp, 0, keyboardMode{kittyFlags: screen.KittyReportAllKeys}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extendedFor(c.key, c.mods, c.mode); got != c.want {
				t.Errorf("extendedFor(%v, %v, %+v) = %v, want %v", c.key, c.mods, c.mode, got, c.want)
			}
		})
	}
}

func TestBaseRune(t *testing.T) {
	cases := []struct {
		key  glfw.Key
		want rune
		ok   bool
	}{
		{glfw.KeyA, 'a', true},
		{glfw.KeyZ, 'z', true},
		{glfw.Key0, '0', true},
		{glfw.KeyBackslash, '\\', true},
		{glfw.KeyLeftBracket, '[', true},
		{glfw.KeySpace, ' ', true},
		{glfw.KeyEnter, 0, false},
		{glfw.KeyUp, 0, false},
		{glfw.KeyTab, 0, false},
	}
	for _, c := range cases {
		got, ok := baseRune(c.key)
		if got != c.want || ok != c.ok {
			t.Errorf("baseRune(%v) = (%q, %v), want (%q, %v)", c.key, got, ok, c.want, c.ok)
		}
	}
}

func TestShiftRune(t *testing.T) {
	cases := []struct{ in, want rune }{
		{'a', 'A'},
		{'z', 'Z'},
		{'1', '!'},
		{'0', ')'},
		{'\\', '|'},
		{'[', '{'},
		{' ', ' '},
	}
	for _, c := range cases {
		if got := shiftRune(c.in); got != c.want {
			t.Errorf("shiftRune(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
