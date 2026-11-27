package screen

// Applet placeholder cells mark where an applet page shows. `tubeless
// exec` fills its pane with them; tmux then clips, moves and covers
// them like any other pane content, and the window paints the page over
// exactly the cells that are still visible.
//
// Each cell carries everything needed to place it, so no cell depends
// on another being visible:
//   - the rune encodes the cell's own column and row in the applet,
//   - the 256-color foreground and background indices encode the
//     applet id. Indices 16-255 survive tmux untouched and are resolved
//     to fixed RGB at parse time, so the id is read back from that RGB.

// appletRuneBase is the first rune of the placeholder range: the start
// of Unicode plane 16, private use, one column wide everywhere.
const appletRuneBase rune = 0x100000

// AppletMaxCols and AppletMaxRows bound an applet's pane. Row 255 is
// left out: its last two runes are noncharacters (U+10FFFE, U+10FFFF).
const (
	AppletMaxCols = 256
	AppletMaxRows = 255
)

// appletColors is how many 256-color indices an id can use per channel.
const appletColors = 240

// AppletMaxID is one past the largest applet id.
const AppletMaxID = appletColors * appletColors

// AppletRune is the placeholder rune for the cell at (col, row) of an
// applet.
func AppletRune(col, row int) rune {
	return appletRuneBase + rune(row<<8|col)
}

// IsAppletRune reports whether r is a placeholder rune.
func IsAppletRune(r rune) bool {
	return r >= appletRuneBase && r < appletRuneBase+AppletMaxRows<<8
}

// AppletColors returns the 256-color foreground and background indices
// that encode id.
func AppletColors(id int) (fg, bg int) {
	return 16 + id/appletColors, 16 + id%appletColors
}

var appletIndexByRGB = func() map[[3]float32]int {
	m := make(map[[3]float32]int, appletColors)
	for i := 0; i < appletColors; i++ {
		m[palette256RGBValue(16+i)] = i
	}
	return m
}()

// AppletCell reports which applet the cell shows and which of its cells
// it is, if it is a placeholder cell at all.
func (c Cell) AppletCell() (id, col, row int, ok bool) {
	if !IsAppletRune(c.Rune) || !c.Attr.FgSet || !c.Attr.BgSet || c.Attr.FgIndexed || c.Attr.BgIndexed {
		return 0, 0, 0, false
	}
	hi, okFg := appletIndexByRGB[c.Attr.FgRGB]
	lo, okBg := appletIndexByRGB[c.Attr.BgRGB]
	if !okFg || !okBg {
		return 0, 0, 0, false
	}
	offset := int(c.Rune - appletRuneBase)
	return hi*appletColors + lo, offset & 0xff, offset >> 8, true
}

// DrainAppletRequests returns and clears every OSC 7777 applet request
// (see handler.go's dispatchOSCApplet), oldest first. cmd/tubeless's
// ptyCoordinator drains it right where it publishes a Screen, so no
// request is lost between two published frames.
func (s *Screen) DrainAppletRequests() [][]byte {
	r := s.pendingApplets
	s.pendingApplets = nil
	return r
}
