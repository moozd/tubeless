// Package sixel decodes DEC's sixel bitmap graphics protocol — the format
// introduced with the VT340 for inline images. This implements the core
// subset real encoders (including cmd/tektest's) actually produce: color
// register definition/selection, run-length repeat, and the 6-pixel-tall
// column data bytes, and their carriage-return/newline separators.
package sixel

import (
	"image"
	"image/color"
)

type mode int

const (
	modeNormal mode = iota
	modeColorParams
	modeRepeatCount
)

// Decoder incrementally parses a sixel data stream (the DCS payload, after
// the introducer and before the ST terminator) into an image. Feed it
// bytes via Put, then call Image once the stream ends.
type Decoder struct {
	palette  map[int]color.RGBA
	curColor color.RGBA

	x, y int

	mode      mode
	numBuf    []int
	curNum    int
	haveDigit bool

	pixels     map[[2]int]color.RGBA
	maxX, maxY int
}

func NewDecoder() *Decoder {
	return &Decoder{
		palette:  map[int]color.RGBA{0: {A: 255}},
		curColor: color.RGBA{R: 255, G: 255, B: 255, A: 255},
		pixels:   make(map[[2]int]color.RGBA),
	}
}

// Put feeds one byte of sixel data into the decoder.
func (d *Decoder) Put(b byte) {
	switch d.mode {
	case modeColorParams:
		d.putColorParam(b)
	case modeRepeatCount:
		d.putRepeatDigit(b)
	default:
		d.putNormal(b)
	}
}

func (d *Decoder) putNormal(b byte) {
	switch {
	case b == '#':
		d.mode = modeColorParams
		d.numBuf = d.numBuf[:0]
		d.curNum, d.haveDigit = 0, false
	case b == '!':
		d.mode = modeRepeatCount
		d.curNum = 0
	case b == '$':
		d.x = 0
	case b == '-':
		d.x = 0
		d.y++
	case b >= '?' && b <= '~':
		d.emit(b - '?')
	}
	// Everything else (notably the '"' raster-attributes command and its
	// numeric params) is intentionally ignored: we size the image from the
	// pixels actually emitted rather than a declared width/height.
}

func (d *Decoder) putColorParam(b byte) {
	switch {
	case b >= '0' && b <= '9':
		d.curNum = d.curNum*10 + int(b-'0')
		d.haveDigit = true
		return
	case b == ';':
		d.numBuf = append(d.numBuf, d.curNum)
		d.curNum, d.haveDigit = 0, false
		return
	}
	if d.haveDigit || len(d.numBuf) > 0 {
		d.numBuf = append(d.numBuf, d.curNum)
	}
	d.applyColorParams(d.numBuf)
	d.mode = modeNormal
	d.putNormal(b)
}

// applyColorParams handles "#Pc" (select register Pc) and
// "#Pc;Pu;Px;Py;Pz" (define register Pc as RGB when Pu==2, then select it).
// HLS (Pu==1) isn't handled — not needed for the encoders we target.
func (d *Decoder) applyColorParams(p []int) {
	if len(p) == 0 {
		return
	}
	pc := p[0]
	if len(p) >= 5 && p[1] == 2 {
		d.palette[pc] = color.RGBA{R: scale100(p[2]), G: scale100(p[3]), B: scale100(p[4]), A: 255}
	}
	if c, ok := d.palette[pc]; ok {
		d.curColor = c
	}
}

func scale100(v int) uint8 {
	switch {
	case v < 0:
		v = 0
	case v > 100:
		v = 100
	}
	return uint8(v * 255 / 100)
}

func (d *Decoder) putRepeatDigit(b byte) {
	if b >= '0' && b <= '9' {
		d.curNum = d.curNum*10 + int(b-'0')
		return
	}
	count := d.curNum
	if count == 0 {
		count = 1
	}
	d.mode = modeNormal
	if b >= '?' && b <= '~' {
		bits := b - '?'
		for range count {
			d.emit(bits)
		}
	}
}

// emit paints one data column: bits is 0-63, bit i set means the pixel at
// row y*6+i is lit in the current color.
func (d *Decoder) emit(bits byte) {
	for i := range 6 {
		if bits&(1<<uint(i)) == 0 {
			continue
		}
		py := d.y*6 + i
		d.pixels[[2]int{d.x, py}] = d.curColor
		d.maxX = max(d.maxX, d.x)
		d.maxY = max(d.maxY, py)
	}
	d.x++
}

// Image returns the decoded picture, sized to the pixels actually emitted.
func (d *Decoder) Image() *image.RGBA {
	if len(d.pixels) == 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	img := image.NewRGBA(image.Rect(0, 0, d.maxX+1, d.maxY+1))
	for pos, c := range d.pixels {
		img.SetRGBA(pos[0], pos[1], c)
	}
	return img
}
