package render

import (
	"github.com/moozd/tubeless/pkg/config"
	"github.com/moozd/tubeless/pkg/screen"
)

// cellColors resolves a cell's attribute into (foreground, background). In
// a TrueColor theme (rosepine) it passes the cell's real RGB straight
// through (see trueColorCell); every other theme interpolates the config's
// single-hue phosphor ramp by intensity instead of introducing separate
// per-color hues — hue can't survive a monochrome theme, but an app's
// fg/bg color still carries real meaning (a git status color, a syntax
// highlight) as long as its *brightness* comes through, which is what
// attr.Fg/Bg (set from SGR color codes, see pkg/screen/sgr.go) give us.
func cellColors(attr screen.Attr, cfg config.Config) (fg, bg [3]float32) {
	if cfg.TrueColor {
		return trueColorCell(attr, cfg)
	}
	fgI := fgIntensity(attr)

	bg = [3]float32{0, 0, 0}
	if !attr.BgSet {
		if attr.Reverse {
			// No explicit background: reverse video is a pure highlight
			// signal (a file-tree selection, a search match, htop's header
			// bar) with no intentional color of its own to preserve —
			// always a clean full invert (solid bright block, black text),
			// never the cell's own original fg brightness. Reusing that
			// brightness reads fine for one uniformly-colored cell, but a
			// highlighted run of text whose characters had different
			// original colors (icons, syntax spans) would then compute a
			// different block shade per cell — a muddy, unevenly-striped
			// bar instead of one solid highlight.
			return [3]float32{0, 0, 0}, cfg.Phosphor.High
		}
		if attr.Invisible {
			return bg, bg
		}
		return lerp3(cfg.Phosphor.Low, cfg.Phosphor.High, fgI), bg
	}

	bgI := scaleIntensity(attr.Bg)
	bg = lerp3(cfg.Phosphor.Low, cfg.Phosphor.High, bgI)
	if attr.Reverse {
		fgI, bgI = bgI, fgI
		bg = lerp3(cfg.Phosphor.Low, cfg.Phosphor.High, bgI)
	}
	if attr.Invisible {
		return bg, bg
	}
	if abs32(fgI-bgI) < cfg.Contrast.MinDelta {
		// Too close to read once collapsed onto one hue's brightness —
		// synthesize a full invert (opposite extremes for both fg and bg)
		// instead of only nudging fg, which can leave two still-similar
		// in-between tones rather than a clean, obviously-highlighted block.
		if bgI >= fgI {
			return [3]float32{0, 0, 0}, cfg.Phosphor.High
		}
		return cfg.Phosphor.High, [3]float32{0, 0, 0}
	}
	return lerp3(cfg.Phosphor.Low, cfg.Phosphor.High, fgI), bg
}

// trueColorCell is cellColors' TrueColor-theme branch: the cell's real RGB
// (attr.FgRGB/BgRGB, set from SGR color codes — see pkg/screen/sgr.go)
// passes straight through instead of collapsing onto a monochrome ramp, so
// contrast isn't auto-enforced here the way the ramp path does — an app
// choosing its own truecolor pair is assumed to have already chosen a
// readable one, same as any other truecolor terminal.
func trueColorCell(attr screen.Attr, cfg config.Config) (fg, bg [3]float32) {
	fg, bg = cfg.Colors.DefaultFg, cfg.Colors.DefaultBg
	if attr.FgSet {
		fg = resolveRGB(attr.FgIndexed, attr.FgIdx, attr.FgRGB, cfg)
	}
	if attr.BgSet {
		bg = resolveRGB(attr.BgIndexed, attr.BgIdx, attr.BgRGB, cfg)
	}
	if attr.Reverse {
		fg, bg = bg, fg
	}
	if attr.Invisible {
		return bg, bg
	}
	if attr.Bold {
		fg = boost3(fg, 0.15)
	}
	if attr.Dim {
		fg = scale3(fg, 0.6)
	}
	return fg, bg
}

// underlineColor is what an underline decoration (see cellpass.go's
// BuildInstances) draws in: the cell's own foreground by default — fg is
// already resolved post-selection-swap by the caller, so the underline
// tracks Reverse/selection the same way the glyph above it does — unless
// SGR 58 set an explicit underline color (Attr.UnderlineColorSet), which
// only has somewhere real to resolve into under a TrueColor theme; the
// monochrome ramp has no separate hue channel for an independent color, so
// it always falls back to fg there.
func underlineColor(attr screen.Attr, fg [3]float32, cfg config.Config) [3]float32 {
	if !cfg.TrueColor || !attr.UnderlineColorSet {
		return fg
	}
	return resolveRGB(attr.UnderlineIndexed, attr.UnderlineIdx, attr.UnderlineRGB, cfg)
}

// resolveRGB returns a cell's actual color: a palette lookup for an
// indexed SGR color (against the *active* theme's Colors.Palette, so a
// live theme switch recolors already-written cells correctly — see
// Attr's doc comment), or the direct RGB already carried on the cell for
// a truecolor/256-cube SGR color.
func resolveRGB(indexed bool, idx int8, direct [3]float32, cfg config.Config) [3]float32 {
	if indexed {
		return cfg.Colors.Palette[idx&0xf]
	}
	return direct
}

// boost3/scale3 are trueColorCell's RGB analogues of fgIntensity's
// Bold/Dim nudges on the monochrome ramp.
func boost3(c [3]float32, amt float32) [3]float32 {
	return [3]float32{min32(1, c[0]+amt), min32(1, c[1]+amt), min32(1, c[2]+amt)}
}

func scale3(c [3]float32, k float32) [3]float32 {
	return [3]float32{c[0] * k, c[1] * k, c[2] * k}
}

// fgIntensity picks where on the phosphor ramp a cell's foreground sits.
// An explicit SGR color takes priority over Bold/Dim's default levels,
// but Bold/Dim still nudge it (a bold-and-colored cell should read
// brighter than a plain one of the same color). The no-color default
// sits well below Bold rather than right next to it — most of a real
// screen (plain body text, an editor's unstyled majority) has no SGR
// color at all, so pinning it near-maximum brightness read as a flat,
// uniformly harsh wall of light with nothing to set genuine emphasis
// (Bold, an explicit bright color) apart from it.
func fgIntensity(attr screen.Attr) float32 {
	if !attr.FgSet {
		switch {
		case attr.Bold:
			return 1.0
		case attr.Dim:
			return 0.45
		default:
			return 0.65
		}
	}
	i := scaleIntensity(attr.Fg)
	if attr.Bold {
		i = min32(1.0, i+0.15)
	}
	if attr.Dim {
		i *= 0.6
	}
	return i
}

// scaleIntensity maps a 0-1 luminance onto a floor..1 range on the
// phosphor ramp — a floor above 0 so an app's "black" foreground (e.g.
// terminal.background-on-background spacing tricks aside) still reads as
// dim phosphor rather than vanishing outright; Invisible is the actual
// mechanism for text that should disappear.
func scaleIntensity(luminance float32) float32 {
	const floor = 0.25
	return floor + (1-floor)*luminance
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func lerp3(a, b [3]float32, t float32) [3]float32 {
	return [3]float32{
		a[0] + (b[0]-a[0])*t,
		a[1] + (b[1]-a[1])*t,
		a[2] + (b[2]-a[2])*t,
	}
}

// rectEdges bundles a rect's per-corner SDF radius with which of its 4
// edges continue into a same-fill neighbor. cellpass.go uses the latter to
// overshoot the rect's own geometry slightly on those edges — otherwise
// two independently anti-aliased instances that are merely flush (not
// overlapping) can each fade out just short of their shared boundary,
// leaving a faint seam even though the corner there is correctly squared.
type rectEdges struct {
	Radii                                 [4]float32
	ContUp, ContRight, ContDown, ContLeft bool
}

// ambientBG is the "empty terminal" backdrop color BuildInstances skips
// drawing a rect for — plain black for the monochrome themes, or
// cfg.Colors.DefaultFg/Bg's background half for a TrueColor theme (see
// Renderer.RenderScene's DrawAmbientBG, which paints the scene with this
// same color before any rects are drawn). Without this, a TrueColor theme
// would draw literally every cell as its own rect, since DefaultBg is
// never the zero value — leaving no transparent space in the glow layer
// for the blur/bloom pass to bleed into (see compositeGlow), so the glow
// effect would only ever show at the literal edge of the window.
func ambientBG(cfg config.Config) [3]float32 {
	if cfg.TrueColor {
		return cfg.Colors.DefaultBg
	}
	return [3]float32{0, 0, 0}
}

// neighborColors is bgRectEdges/blockRectEdges' shared neighbor lookup:
// cellColors for the cell at (nx,ny), preferring an already-computed
// cache entry over a redundant call. fgCache/bgCache hold every
// already-visited cell's own cellColors result (BuildInstances fills them
// in as it sweeps the grid row-major, x innermost) — a neighbor above
// (ny<y) or to the left on the same row (ny==y && nx<x) of the cell
// currently being processed at (x,y) is always already visited by
// construction, so its color comes from the cache instead of recomputing;
// a neighbor to the right or below isn't visited yet and still computes
// directly, same as before this cache existed.
func neighborColors(grid [][]screen.Cell, fgCache, bgCache [][3]float32, cols int, cfg config.Config, x, y, nx, ny int) (fg, bg [3]float32) {
	if ny < y || (ny == y && nx < x) {
		i := ny*cols + nx
		return fgCache[i], bgCache[i]
	}
	return cellColors(grid[ny][nx].Attr, cfg)
}

// bgRectEdges computes rectEdges for a background fill at cell (x,y).
// grid/cols/rows are the currently-visible window (see
// screen.Screen.VisibleWindow) rather than the live Screen directly, so
// neighbor lookups respect the same scrolled-back view BuildInstances is
// drawing, not necessarily the screen's live tail. fgCache/bgCache are
// BuildInstances' per-cell color cache — see neighborColors.
func bgRectEdges(grid [][]screen.Cell, cols, rows int, fgCache, bgCache [][3]float32, cfg config.Config, x, y int, bg [3]float32) rectEdges {
	same := func(dx, dy int) bool {
		nx, ny := x+dx, y+dy
		if nx < 0 || nx >= cols || ny < 0 || ny >= rows {
			return false
		}
		_, nbg := neighborColors(grid, fgCache, bgCache, cols, cfg, x, y, nx, ny)
		return nbg == bg
	}
	up, right, down, left := same(0, -1), same(1, 0), same(0, 1), same(-1, 0)
	return rectEdges{
		Radii:     cornerRadii(cfg.Rounding.Radius, true, true, true, true, up, right, down, left),
		ContUp:    up,
		ContRight: right,
		ContDown:  down,
		ContLeft:  left,
	}
}

// blockRectEdges is bgRectEdges' counterpart for a single-rect block glyph
// (font.BlockRect): "continues" additionally requires the neighbor to be
// the exact same rune and foreground color — a different block shape or
// color can't visually continue the same rectangle. An edge that isn't on
// the glyph's own sub-rect boundary (x0/y0/x1/y1 against the cell edge)
// never counts as continuing, since nothing can continue across a boundary
// internal to the same cell (e.g. the bottom edge of ▀, the upper-half
// block) — its corners always round, and it's never overshot.
func blockRectEdges(grid [][]screen.Cell, cols, rows int, fgCache, bgCache [][3]float32, cfg config.Config, x, y int, r rune, fg [3]float32, x0, y0, x1, y1 float32) rectEdges {
	same := func(dx, dy int) bool {
		nx, ny := x+dx, y+dy
		if nx < 0 || nx >= cols || ny < 0 || ny >= rows {
			return false
		}
		n := grid[ny][nx]
		if n.Rune != r {
			return false
		}
		nfg, _ := neighborColors(grid, fgCache, bgCache, cols, cfg, x, y, nx, ny)
		return nfg == fg
	}
	touchTop, touchRight := y0 == 0, x1 == 1
	touchBottom, touchLeft := y1 == 1, x0 == 0
	up, right, down, left := same(0, -1), same(1, 0), same(0, 1), same(-1, 0)
	return rectEdges{
		Radii:     cornerRadii(cfg.Rounding.Radius, touchTop, touchRight, touchBottom, touchLeft, up, right, down, left),
		ContUp:    touchTop && up,
		ContRight: touchRight && right,
		ContDown:  touchBottom && down,
		ContLeft:  touchLeft && left,
	}
}

// cornerRadii returns the SDF corner radius (TL, TR, BR, BL) for a filled
// rect. A corner only rounds when it's a genuine convex corner of the
// overall filled shape: on a true cell-boundary edge (touch*) AND with
// BOTH adjacent neighbors failing to continue the same fill — an edge cell
// in the middle of a run has one neighbor that does continue (e.g. the
// cell to its left, in a horizontal bar), which keeps that side square so
// the run reads as one continuous straight edge, only rounding at the
// run's actual ends, instead of scalloping at every internal seam.
func cornerRadii(radius float32, touchTop, touchRight, touchBottom, touchLeft, contUp, contRight, contDown, contLeft bool) [4]float32 {
	sq := func(touchA, touchB, contA, contB bool) float32 {
		if touchA && touchB && (contA || contB) {
			return 0
		}
		return radius
	}
	return [4]float32{
		sq(touchTop, touchLeft, contUp, contLeft),
		sq(touchTop, touchRight, contUp, contRight),
		sq(touchBottom, touchRight, contDown, contRight),
		sq(touchBottom, touchLeft, contDown, contLeft),
	}
}
