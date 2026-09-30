package screen

// SlotRune marks a terminal cell as belonging to an embedded GUI app's
// pane (see cmd/tubeless's slots). The pane process fills its whole
// rectangle with this rune, colored by SlotColorIndex so the window can
// tell which app each cell belongs to; tmux then clips, moves and hides
// the cells like any other pane content, and the window paints the app's
// pixels over exactly the cells that are still visible.
const SlotRune rune = 0x10EEEE

// MaxSlots is how many embedded apps one window can host: the 240 colors
// of the xterm 256-color palette above the 16 ANSI ones. Those survive
// tmux untouched and are resolved to fixed RGB at parse time.
const MaxSlots = 240

// SlotColorIndex is the 256-color palette index whose foreground color
// encodes slot id (0 <= id < MaxSlots).
func SlotColorIndex(id int) int {
	return 16 + id
}

var slotByRGB = func() map[[3]float32]int {
	m := make(map[[3]float32]int, MaxSlots)
	for id := 0; id < MaxSlots; id++ {
		m[palette256RGBValue(SlotColorIndex(id))] = id
	}
	return m
}()

// SlotID reports which embedded app the cell belongs to, if it is a
// slot-marker cell at all.
func (c Cell) SlotID() (int, bool) {
	if c.Rune != SlotRune || !c.Attr.FgSet || c.Attr.FgIndexed {
		return 0, false
	}
	id, ok := slotByRGB[c.Attr.FgRGB]
	return id, ok
}
