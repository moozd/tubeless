package main

import (
	"image"
	"image/color"
	"strings"

	sixelenc "github.com/mattn/go-sixel"
)

const (
	waveImgWidth  = 480
	waveImgHeight = 190
	waveLineWidth = 3
)

var traceColor = color.RGBA{R: 40, G: 235, B: 200, A: 255}

// writeWaveformImage renders the stepped square wave as a real bitmap
// (ASCII line art can't produce the reference photo's smooth continuous
// trace) and emits it as a sixel image at the box interior's top-left.
func writeWaveformImage(out *strings.Builder) {
	img := renderWaveformImage()
	out.WriteString(moveTo(waveTop, waveLeft))
	enc := sixelenc.NewEncoder(out)
	enc.Encode(img)
}

func renderWaveformImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, waveImgWidth, waveImgHeight))
	highY := waveImgHeight * 20 / 100
	lowY := waveImgHeight * 80 / 100
	colWidth := float64(waveImgWidth) / float64(waveWidth)

	prevHigh := true
	prevX := 0
	for x := range waveWidth {
		high := (x % wavePeriod) < waveDuty
		px := int(float64(x+1) * colWidth)
		y := highY
		if !high {
			y = lowY
		}
		if high != prevHigh {
			fillRect(img, int(float64(x)*colWidth)-waveLineWidth/2, highY, waveLineWidth, lowY-highY)
		}
		fillRect(img, prevX, y-waveLineWidth/2, px-prevX, waveLineWidth)
		prevX = px
		prevHigh = high
	}
	return img
}

func fillRect(img *image.RGBA, x, y, w, h int) {
	if w < 0 {
		x, w = x+w, -w
	}
	for yy := y; yy < y+h; yy++ {
		if yy < 0 || yy >= img.Bounds().Dy() {
			continue
		}
		for xx := x; xx < x+w; xx++ {
			if xx < 0 || xx >= img.Bounds().Dx() {
				continue
			}
			img.SetRGBA(xx, yy, traceColor)
		}
	}
}
