package main

import (
	"bytes"
	"strconv"
	"unicode/utf8"
)

// Modifier bits as the xterm/kitty "modifier parameter" encodes them
// (parameter value minus one).
const (
	modShift = 1 << iota
	modAlt
	modCtrl
	modSuper
)

// X11 keysyms for the keys a terminal can report.
const (
	keysymBackSpace = 0xff08
	keysymTab       = 0xff09
	keysymReturn    = 0xff0d
	keysymEscape    = 0xff1b
	keysymHome      = 0xff50
	keysymLeft      = 0xff51
	keysymUp        = 0xff52
	keysymRight     = 0xff53
	keysymDown      = 0xff54
	keysymPageUp    = 0xff55
	keysymPageDown  = 0xff56
	keysymEnd       = 0xff57
	keysymInsert    = 0xff63
	keysymF1        = 0xffbe
	keysymDelete    = 0xffff
	keysymLeftTab   = 0xfe20
	keysymShiftL    = 0xffe1
	keysymControlL  = 0xffe3
	keysymAltL      = 0xffe9
	keysymSuperL    = 0xffeb
)

// inputEvent is one thing decoded from the pane's stdin: a key, or a
// focus change tmux reported.
type inputEvent struct {
	keysym  uint32
	mods    int
	focus   bool // set for focus events; keysym is unused
	focused bool
}

// keyDecoder turns the raw bytes tmux writes to a pane's stdin (legacy
// sequences, xterm modifyOtherKeys, CSI u) into key events. Mouse
// reports and bracketed-paste markers are consumed and dropped: the pane
// process asks for mouse reporting only so tmux stops treating drags as
// copy-mode selections.
type keyDecoder struct {
	pending []byte
}

// Feed decodes everything it can; an incomplete trailing sequence stays
// buffered for the next Feed (or Flush).
func (d *keyDecoder) Feed(b []byte) []inputEvent {
	d.pending = append(d.pending, b...)
	var out []inputEvent
	for len(d.pending) > 0 {
		ev, n, ok := decodeOne(d.pending)
		if !ok {
			break
		}
		d.pending = d.pending[n:]
		if ev != nil {
			out = append(out, *ev)
		}
	}
	return out
}

// Flush gives up waiting for the rest of a sequence: a lone ESC that
// nothing followed is the Escape key.
func (d *keyDecoder) Flush() []inputEvent {
	if len(d.pending) == 0 {
		return nil
	}
	if !bytes.Equal(d.pending, []byte{0x1b}) {
		d.pending = nil
		return nil
	}
	d.pending = nil
	return []inputEvent{{keysym: keysymEscape}}
}

func (d *keyDecoder) HasPending() bool {
	return len(d.pending) > 0
}

// decodeOne reads one event from the front of b. ok=false means b holds
// only part of a sequence. A nil event with ok=true is input to drop.
func decodeOne(b []byte) (ev *inputEvent, n int, ok bool) {
	switch c := b[0]; {
	case c == 0x1b:
		return decodeEscape(b)
	case c == 0x0d:
		return key(keysymReturn, 0), 1, true
	case c == 0x09:
		return key(keysymTab, 0), 1, true
	case c == 0x7f || c == 0x08:
		return key(keysymBackSpace, 0), 1, true
	case c == 0x00:
		return key(' ', modCtrl), 1, true
	case c < 0x20:
		return key(uint32('a'+c-1), modCtrl), 1, true
	}
	r, size := utf8.DecodeRune(b)
	if r == utf8.RuneError && size <= 1 && !utf8.FullRune(b) {
		return nil, 0, false
	}
	return key(runeKeysym(r), 0), size, true
}

func key(sym uint32, mods int) *inputEvent {
	return &inputEvent{keysym: sym, mods: mods}
}

func runeKeysym(r rune) uint32 {
	if r < 0x100 {
		return uint32(r)
	}
	return 0x01000000 + uint32(r)
}

func decodeEscape(b []byte) (*inputEvent, int, bool) {
	if len(b) == 1 {
		return nil, 0, false
	}
	switch b[1] {
	case '[':
		return decodeCSI(b)
	case 'O':
		if len(b) < 3 {
			return nil, 0, false
		}
		return ss3Key(b[2]), 3, true
	}
	ev, n, ok := decodeOne(b[1:])
	if !ok || ev == nil {
		return nil, 0, ok
	}
	ev.mods |= modAlt
	return ev, n + 1, true
}

func ss3Key(c byte) *inputEvent {
	if c >= 'P' && c <= 'S' {
		return key(keysymF1+uint32(c-'P'), 0)
	}
	return csiCursorKey(c, 0)
}

func csiCursorKey(final byte, mods int) *inputEvent {
	syms := map[byte]uint32{
		'A': keysymUp, 'B': keysymDown, 'C': keysymRight, 'D': keysymLeft,
		'H': keysymHome, 'F': keysymEnd,
	}
	if sym, ok := syms[final]; ok {
		return key(sym, mods)
	}
	return nil
}

// decodeCSI parses ESC [ params final.
func decodeCSI(b []byte) (*inputEvent, int, bool) {
	i := 2
	for i < len(b) && (b[i] < 0x40 || b[i] > 0x7e) {
		i++
	}
	if i >= len(b) {
		return nil, 0, false
	}
	params, final := string(b[2:i]), b[i]
	n := i + 1
	if len(params) > 0 && (params[0] == '<' || params[0] == '?' || params[0] == '>') {
		return nil, n, true // mouse report or a reply we never asked for
	}
	nums := splitParams(params)
	mods := 0
	if len(nums) >= 2 && nums[1] > 1 {
		mods = nums[1] - 1
	}
	switch final {
	case 'I', 'O':
		if params == "" {
			return &inputEvent{focus: true, focused: final == 'I'}, n, true
		}
	case 'A', 'B', 'C', 'D', 'H', 'F':
		return csiCursorKey(final, mods), n, true
	case 'Z':
		return key(keysymLeftTab, 0), n, true
	case '~':
		return decodeTilde(nums, mods), n, true
	case 'u':
		if len(nums) >= 1 {
			return key(csiUKeysym(nums[0]), mods), n, true
		}
	}
	return nil, n, true
}

func splitParams(s string) []int {
	var out []int
	for _, f := range bytes.Split([]byte(s), []byte{';'}) {
		first, _, _ := bytes.Cut(f, []byte{':'})
		v, err := strconv.Atoi(string(first))
		if err != nil {
			v = 0
		}
		out = append(out, v)
	}
	return out
}

func decodeTilde(nums []int, mods int) *inputEvent {
	if len(nums) == 0 {
		return nil
	}
	if nums[0] == 27 && len(nums) >= 3 { // modifyOtherKeys: CSI 27;mod;code ~
		m := 0
		if nums[1] > 1 {
			m = nums[1] - 1
		}
		return key(csiUKeysym(nums[2]), m)
	}
	if nums[0] == 200 || nums[0] == 201 { // bracketed paste markers
		return nil
	}
	tilde := map[int]uint32{
		1: keysymHome, 2: keysymInsert, 3: keysymDelete, 4: keysymEnd,
		5: keysymPageUp, 6: keysymPageDown, 7: keysymHome, 8: keysymEnd,
		11: keysymF1, 12: keysymF1 + 1, 13: keysymF1 + 2, 14: keysymF1 + 3, 15: keysymF1 + 4,
		17: keysymF1 + 5, 18: keysymF1 + 6, 19: keysymF1 + 7, 20: keysymF1 + 8, 21: keysymF1 + 9,
		23: keysymF1 + 10, 24: keysymF1 + 11,
	}
	if sym, ok := tilde[nums[0]]; ok {
		return key(sym, mods)
	}
	return nil
}

// csiUKeysym maps a CSI u / modifyOtherKeys key code to a keysym.
func csiUKeysym(code int) uint32 {
	switch code {
	case 13:
		return keysymReturn
	case 9:
		return keysymTab
	case 27:
		return keysymEscape
	case 127:
		return keysymBackSpace
	}
	return runeKeysym(rune(code))
}

// keyTap is one press or release in an expanded key sequence.
type keyTap struct {
	keysym  uint32
	pressed bool
}

// keyTaps expands one decoded key into the press/release sequence a real
// keyboard would produce, modifiers bracketing the key. tmux delivers
// only presses, so the release is synthesized right after.
func keyTaps(ev inputEvent) []keyTap {
	held := []struct {
		bit int
		sym uint32
	}{{modShift, keysymShiftL}, {modCtrl, keysymControlL}, {modAlt, keysymAltL}, {modSuper, keysymSuperL}}
	var out []keyTap
	var down []uint32
	for _, m := range held {
		if ev.mods&m.bit != 0 {
			out = append(out, keyTap{m.sym, true})
			down = append(down, m.sym)
		}
	}
	out = append(out, keyTap{ev.keysym, true}, keyTap{ev.keysym, false})
	for i := len(down) - 1; i >= 0; i-- {
		out = append(out, keyTap{down[i], false})
	}
	return out
}
