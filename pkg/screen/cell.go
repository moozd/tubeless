package screen

// Attr holds SGR-derived rendering state for one cell. The default theme
// is a single-hue phosphor ramp, so an app's fg/bg color can't be
// reproduced as hue there — but dropping it entirely loses real
// information (a git status color, a syntax-highlight color) that the app
// is using to draw the eye. Fg/Bg carry the *luminance* (0-1) of whatever
// color the app set, which the monochrome renderer maps onto the phosphor
// ramp's brightness — hue is lost, but bright/dim distinctions the app is
// relying on survive. FgRGB/BgRGB carry the same color's real RGB (0-1),
// for a truecolor theme (see pkg/config's TrueColor) that renders it
// directly instead of collapsing it onto the ramp — unless FgIndexed/
// BgIndexed is set, in which case FgIdx/BgIdx (0-15) name a slot in the
// active theme's 16-color ANSI palette instead of a fixed RGB: an indexed
// SGR color (30-37/40-47/90-97/100-107, or 38;5;n/48;5;n with n<16) is
// deliberately *not* resolved to RGB here at parse time, so a live theme
// switch recolors already-written cells too — see pkg/config's
// Colors.Palette and pkg/render's trueColorCell, which does the resolve.
type Attr struct {
	Bold      bool
	Dim       bool
	Underline UnderlineStyle
	Blink     bool
	Reverse   bool
	Invisible bool
	FgSet     bool
	FgIndexed bool
	FgIdx     int8
	Fg        float32
	FgRGB     [3]float32
	BgSet     bool
	BgIndexed bool
	BgIdx     int8
	Bg        float32
	BgRGB     [3]float32

	// UnderlineColorSet is SGR 58 (59 clears it): an explicit underline
	// color, independent of Fg — a spellcheck squiggle drawn in red under
	// otherwise plain-colored text, say. Unset (the common case) means the
	// underline draws in the cell's own foreground color instead. Same
	// indexed-vs-direct-RGB split as Fg/FgIndexed/FgIdx/FgRGB, and (see
	// pkg/render's resolveRGB) resolved the same way.
	UnderlineColorSet bool
	UnderlineIndexed  bool
	UnderlineIdx      int8
	UnderlineRGB      [3]float32
}

// UnderlineStyle is which underline decoration (if any) SGR 4 selected —
// none, or 4:0/4:1/4:2/4:3/4:4/4:5's extended sub-styles (a bare "CSI 4m"
// with no sub-parameter is UnderlineSingle, matching every real terminal).
type UnderlineStyle uint8

const (
	UnderlineNone UnderlineStyle = iota
	UnderlineSingle
	UnderlineDouble
	UnderlineCurly
	UnderlineDotted
	UnderlineDashed
)

// Cell is one grid position: a rune plus the attributes it was written with.
type Cell struct {
	Rune rune
	Attr Attr
}

func blankCell() Cell {
	return Cell{Rune: ' '}
}

// erasedCell is what EL/ED/ECH fill freed positions with: ECMA-48 (and
// every real terminal — xterm, kitty, alacritty) erases to the *current*
// SGR background, not a hardcoded default. That's what lets an app like
// nvim-tree draw a full-width selection bar by setting a background color
// and erasing to end-of-line, instead of writing an actual space over
// every remaining cell — filling with a plain blankCell() here made that
// highlight stop dead at the last character it actually wrote.
func erasedCell(attr Attr) Cell {
	return Cell{Rune: ' ', Attr: Attr{
		Reverse:   attr.Reverse,
		BgSet:     attr.BgSet,
		BgIndexed: attr.BgIndexed,
		BgIdx:     attr.BgIdx,
		Bg:        attr.Bg,
		BgRGB:     attr.BgRGB,
	}}
}
