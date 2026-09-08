package screen

// Attr holds SGR-derived rendering state for one cell. The theme is a
// single-hue phosphor ramp, so an app's fg/bg color can't be reproduced
// as hue — but dropping it entirely loses real information (a git status
// color, a syntax-highlight color) that the app is using to draw the
// eye. Fg/Bg carry the *luminance* (0-1) of whatever color the app set,
// which the renderer maps onto the phosphor ramp's brightness — hue is
// lost, but bright/dim distinctions the app is relying on survive.
type Attr struct {
	Bold      bool
	Dim       bool
	Underline bool
	Blink     bool
	Reverse   bool
	Invisible bool
	FgSet     bool
	Fg        float32
	BgSet     bool
	Bg        float32
}

// Cell is one grid position: a rune plus the attributes it was written with.
type Cell struct {
	Rune rune
	Attr Attr
}

func blankCell() Cell {
	return Cell{Rune: ' '}
}
