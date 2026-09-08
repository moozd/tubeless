// Command tektest emits the exact TDS-420 oscilloscope screen content from
// the reference photo (~/tds-420.JPG) via plain VT text-mode sequences, so
// it can be run inside tubeless as a fixed fidelity target for tuning the
// green (Tektronix) CRT shader against the real photo.
package main

import (
	"fmt"
	"os"
	"strings"
)

const (
	reverseOn  = "\x1b[7m"
	reverseOff = "\x1b[0m"
	boldOn     = "\x1b[1m"
	boldOff    = "\x1b[22m"
)

func moveTo(row, col int) string {
	return fmt.Sprintf("\x1b[%d;%dH", row, col)
}

func main() {
	out := strings.Builder{}
	out.WriteString("\x1b[2J")
	writeStatusLine(&out)
	writeWaveform(&out)
	writeChannelReadout(&out)
	writeMenu(&out)
	writeMeasurementLine(&out)
	writeButtonBar(&out)
	out.WriteString(moveTo(30, 1))
	os.Stdout.WriteString(out.String())
	blockForever()
}

// blockForever waits on stdin so the process (and the PTY session showing
// its output) stays alive for visual comparison until the terminal closes.
func blockForever() {
	buf := make([]byte, 1)
	for {
		if _, err := os.Stdin.Read(buf); err != nil {
			return
		}
	}
}

func writeStatusLine(out *strings.Builder) {
	out.WriteString(moveTo(1, 3))
	out.WriteString("Tek Run: 100kS/s")
	out.WriteString(moveTo(1, 30))
	out.WriteString("Hi Res")
}

func writeChannelReadout(out *strings.Builder) {
	lines := []string{
		"Ch1 Freq",
		"  1.00008kHz",
		"Low signal amplitude",
		"Ch1 High",
		"  520mV",
		"Ch1 Pk-Pk",
		"  500mV",
		"Ch1 +Duty",
		"  50.2%",
		"Low signal amplitude",
	}
	for i, line := range lines {
		out.WriteString(moveTo(3+i, 46))
		out.WriteString(line)
	}
}

func writeMenu(out *strings.Builder) {
	out.WriteString(moveTo(2, 63))
	out.WriteString(boldOn + "Acquisition" + boldOff)
	out.WriteString(moveTo(3, 65))
	out.WriteString(boldOn + "Mode" + boldOff)

	items := []struct {
		row       int
		label     string
		highlight bool
	}{
		{6, "Sample", false},
		{10, "Peak Detect", false},
		{14, "Hi Res", true},
		{18, "Envelope   10", false},
		{22, "Average    16", false},
	}
	for _, it := range items {
		writeMenuItem(out, it.row, it.label, it.highlight)
	}
}

func writeMenuItem(out *strings.Builder, row int, label string, highlight bool) {
	out.WriteString(moveTo(row, 62))
	padded := fmt.Sprintf(" %-13s", label)
	if highlight {
		out.WriteString(reverseOn + padded + reverseOff)
		return
	}
	out.WriteString(padded)
}

func writeMeasurementLine(out *strings.Builder) {
	out.WriteString(moveTo(25, 2))
	out.WriteString("CH1  500mV     Ch2  200mV     M 500µs     Ch1 ↓  -2.40V")
}

func writeButtonBar(out *strings.Builder) {
	buttons := [][2]string{
		{"Mode", "Hi Res"},
		{"Repeat Sig", "On"},
		{"Stop After", "R/S btn"},
		{"", ""},
		{"Limit Test", "Setup"},
		{"Limit Test", "Sources"},
		{"Create Lmt", "Template"},
	}
	col := 2
	for _, b := range buttons {
		out.WriteString(moveTo(27, col))
		out.WriteString(b[0])
		out.WriteString(moveTo(28, col))
		out.WriteString(b[1])
		col += 12
	}
}
