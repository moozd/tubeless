package screen

import "image"

// PlacedImage is a decoded sixel image anchored at a grid cell — the
// renderer is responsible for knowing the physical pixel size of a cell
// and blitting accordingly; Screen just remembers where images go.
type PlacedImage struct {
	Col, Row int
	Img      *image.RGBA
}
