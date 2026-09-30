package main

import (
	"reflect"
	"testing"
)

func decodeAll(input string) []inputEvent {
	var d keyDecoder
	return d.Feed([]byte(input))
}

func TestDecodePrintableAndControl(t *testing.T) {
	got := decodeAll("a\x01\r\x7fé中")
	want := []inputEvent{
		{keysym: 'a'},
		{keysym: 'a', mods: modCtrl},
		{keysym: keysymReturn},
		{keysym: keysymBackSpace},
		{keysym: 0xe9},
		{keysym: 0x01000000 + 0x4e2d},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestDecodeCursorAndFunctionKeys(t *testing.T) {
	got := decodeAll("\x1b[A\x1b[1;5C\x1bOP\x1b[15~\x1b[3;2~\x1b[Z")
	want := []inputEvent{
		{keysym: keysymUp},
		{keysym: keysymRight, mods: modCtrl},
		{keysym: keysymF1},
		{keysym: keysymF1 + 4},
		{keysym: keysymDelete, mods: modShift},
		{keysym: keysymLeftTab},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestDecodeExtendedKeyFormats(t *testing.T) {
	got := decodeAll("\x1b[27;5;99~\x1b[99;6u\x1bx")
	want := []inputEvent{
		{keysym: 'c', mods: modCtrl},
		{keysym: 'c', mods: modCtrl | modShift},
		{keysym: 'x', mods: modAlt},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestDecodeFocusAndDropsMouseAndPasteMarkers(t *testing.T) {
	got := decodeAll("\x1b[I\x1b[<0;10;5M\x1b[<0;10;5m\x1b[200~h\x1b[201~\x1b[O")
	want := []inputEvent{
		{focus: true, focused: true},
		{keysym: 'h'},
		{focus: true, focused: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestDecodeBuffersSplitSequencesAndFlushesLoneEscape(t *testing.T) {
	var d keyDecoder
	if got := d.Feed([]byte("\x1b[1;")); len(got) != 0 {
		t.Fatalf("partial CSI produced %+v", got)
	}
	got := d.Feed([]byte("5D"))
	if want := []inputEvent{{keysym: keysymLeft, mods: modCtrl}}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	d.Feed([]byte{0x1b})
	if !d.HasPending() {
		t.Fatal("a lone ESC should wait for a possible sequence")
	}
	if got := d.Flush(); !reflect.DeepEqual(got, []inputEvent{{keysym: keysymEscape}}) {
		t.Errorf("Flush = %+v, want Escape", got)
	}
}

func TestKeyTapsBracketModifiers(t *testing.T) {
	got := keyTaps(inputEvent{keysym: 'c', mods: modCtrl | modShift})
	want := []keyTap{
		{keysymShiftL, true}, {keysymControlL, true},
		{'c', true}, {'c', false},
		{keysymControlL, false}, {keysymShiftL, false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
