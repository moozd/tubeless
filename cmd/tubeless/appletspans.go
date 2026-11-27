package main

import (
	"github.com/moozd/tubeless/pkg/render"
	"github.com/moozd/tubeless/pkg/screen"
)

// appletGrid is an applet's pane size in cells.
type appletGrid struct{ cols, rows int }

// appletSpans finds every visible placeholder cell of an open applet
// and merges neighbors into runs. Each cell names its own position in
// the applet, so a run's texture coordinates come from its first cell:
// a pane half covered by a tmux popup still maps exactly.
func appletSpans(scr *screen.Screen, sizes map[int]appletGrid) []render.AppletSpan {
	if len(sizes) == 0 {
		return nil
	}
	var spans []render.AppletSpan
	for y, row := range scr.Grid {
		spans = appendRowSpans(spans, row, y, sizes)
	}
	return spans
}

func appendRowSpans(spans []render.AppletSpan, row []screen.Cell, y int, sizes map[int]appletGrid) []render.AppletSpan {
	var run *render.AppletSpan
	lastCol := 0
	for x, cell := range row {
		id, col, cellRow, ok := cell.AppletCell()
		g, isOpen := sizes[id]
		if !ok || !isOpen || col >= g.cols || cellRow >= g.rows {
			run = nil
			continue
		}
		if run != nil && run.ID == id && col == lastCol+1 {
			run.Cells++
			run.U1 = float32(col+1) / float32(g.cols)
			lastCol = col
			continue
		}
		spans = append(spans, newAppletSpan(id, x, y, col, cellRow, g))
		run = &spans[len(spans)-1]
		lastCol = col
	}
	return spans
}

func newAppletSpan(id, x, y, col, row int, g appletGrid) render.AppletSpan {
	return render.AppletSpan{
		ID: id, Col: x, Row: y, Cells: 1,
		U0: float32(col) / float32(g.cols), V0: float32(row) / float32(g.rows),
		U1: float32(col+1) / float32(g.cols), V1: float32(row+1) / float32(g.rows),
	}
}
