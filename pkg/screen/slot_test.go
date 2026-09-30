package screen

import (
	"fmt"
	"testing"

	"github.com/moozd/tubeless/pkg/vtparse"
)

// slotCellAfter feeds the exact bytes a pane process prints for a slot
// marker and returns the resulting cell.
func slotCellAfter(id int) Cell {
	s := New(10, 2)
	p := vtparse.New(NewHandler(s))
	p.Write([]byte(fmt.Sprintf("\x1b[38;5;%dm%c\x1b[0m", SlotColorIndex(id), SlotRune)))
	return s.Grid[0][0]
}

func TestSlotIDRoundTripsThroughTheParser(t *testing.T) {
	for _, id := range []int{0, 1, 77, MaxSlots - 1} {
		got, ok := slotCellAfter(id).SlotID()
		if !ok || got != id {
			t.Errorf("id %d: got (%d, %v)", id, got, ok)
		}
	}
}

func TestSlotIDIgnoresOrdinaryCells(t *testing.T) {
	s := New(10, 2)
	p := vtparse.New(NewHandler(s))
	p.Write([]byte("\x1b[38;5;20mx\x1b[0m" + string(SlotRune)))
	if _, ok := s.Grid[0][0].SlotID(); ok {
		t.Error("a plain rune was taken for a slot cell")
	}
	if _, ok := s.Grid[0][1].SlotID(); ok {
		t.Error("an uncolored slot rune was taken for a slot cell")
	}
}
