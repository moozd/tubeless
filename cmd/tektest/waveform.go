package main

import "strings"

const (
	waveTop    = 3
	waveLeft   = 3
	waveWidth  = 40
	waveHigh   = 4  // row offset of the high level
	waveLow    = 15 // row offset of the low level
	wavePeriod = 8
	waveDuty   = 4
)

// writeWaveform draws the border box, graticule and channel markers as
// text, then the stepped square wave itself as a real sixel bitmap — ASCII
// line art can't reproduce the reference photo's smooth continuous trace.
func writeWaveform(out *strings.Builder) {
	writeBox(out, waveTop-1, waveLeft-1, waveWidth+2, waveLow+2)
	writeGraticule(out, waveTop, waveLeft, waveWidth, waveLow)
	writeChannelMarkers(out)
	writeWaveformImage(out)
}

// writeGraticule draws the scope's dim dotted division grid inside the
// waveform box, visible in the reference photo but previously missing here.
func writeGraticule(out *strings.Builder, top, left, w, h int) {
	const dimOn, dimOff = "\x1b[2m", "\x1b[22m"
	colStep, rowStep := 4, 2
	out.WriteString(dimOn)
	for y := rowStep; y < h; y += rowStep {
		for x := colStep; x < w; x += colStep {
			writeCell(out, top+y, left+x, '·')
		}
	}
	out.WriteString(dimOff)
}

func writeChannelMarkers(out *strings.Builder) {
	out.WriteString(moveTo(waveTop+waveHigh, waveLeft-2))
	out.WriteString("1")
	out.WriteString(moveTo(waveTop+waveLow, waveLeft-2))
	out.WriteString("2+")
}

func writeCell(out *strings.Builder, row, col int, r rune) {
	out.WriteString(moveTo(row, col))
	out.WriteRune(r)
}

func writeBox(out *strings.Builder, top, left, w, h int) {
	writeCell(out, top, left, '┌')
	writeCell(out, top, left+w, '┐')
	writeCell(out, top+h, left, '└')
	writeCell(out, top+h, left+w, '┘')
	writeHorizontalEdge(out, top, left, w)
	writeHorizontalEdge(out, top+h, left, w)
	writeVerticalEdge(out, top, left, h)
	writeVerticalEdge(out, top, left+w, h)
}

func writeHorizontalEdge(out *strings.Builder, row, left, w int) {
	out.WriteString(moveTo(row, left+1))
	for i := 1; i < w; i++ {
		out.WriteRune('─')
	}
}

func writeVerticalEdge(out *strings.Builder, top, col, h int) {
	for i := 1; i < h; i++ {
		writeCell(out, top+i, col, '│')
	}
}
