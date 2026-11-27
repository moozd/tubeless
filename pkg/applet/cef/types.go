package cef

import "errors"

// ErrUnavailable is what every call returns in a build without the
// `cef` tag.
var ErrUnavailable = errors.New("cef: tubeless was built without applet support (build tag cef)")

// Options locate the CEF runtime files.
type Options struct {
	// SubprocessPath is the tubeless-cef-helper binary CEF starts for
	// its renderer and GPU processes.
	SubprocessPath string
	ResourcesDir   string
	LocalesDir     string
	CacheDir       string
	LogFile        string
	// Background is the ARGB color a page shows before it paints.
	Background uint32
}

// SchemeRequest is one tubeless:// request from a page.
type SchemeRequest struct {
	// BrowserID is the requesting page's ID (see Browser.ID), or -1.
	BrowserID int
	URL       string
	Method    string
	Body      []byte
}

// SchemeResponse answers a SchemeRequest.
type SchemeResponse struct {
	Status int
	MIME   string
	Body   []byte
}

// Rect is a dirty rectangle in frame pixels.
type Rect struct {
	X, Y, W, H int
}

// Frame is a page's latest pixels: BGRA, W x H, premultiplied, sRGB.
// Dirty lists the rects that changed since the previous TakeFrame.
type Frame struct {
	W, H  int
	Pix   []byte
	Dirty []Rect
}

// store copies the changed rects of a CEF paint into the frame. A size
// change replaces the full frame.
func (f *Frame) store(rects []int32, pix []byte, w, h int) {
	if w != f.W || h != f.H || len(f.Pix) != len(pix) {
		f.W, f.H = w, h
		f.Pix = append(f.Pix[:0], pix...)
		f.Dirty = append(f.Dirty[:0], Rect{0, 0, w, h})
		return
	}
	for i := 0; i+3 < len(rects); i += 4 {
		r := Rect{int(rects[i]), int(rects[i+1]), int(rects[i+2]), int(rects[i+3])}
		f.copyRect(r, pix)
		f.Dirty = append(f.Dirty, r)
	}
}

func (f *Frame) copyRect(r Rect, pix []byte) {
	stride := f.W * 4
	for y := r.Y; y < r.Y+r.H; y++ {
		row := y*stride + r.X*4
		copy(f.Pix[row:row+r.W*4], pix[row:row+r.W*4])
	}
}

// MouseButton matches cef_mouse_button_type_t.
type MouseButton int

const (
	MouseLeft MouseButton = iota
	MouseMiddle
	MouseRight
)

// KeyType matches cef_key_event_type_t.
type KeyType int

const (
	KeyRawDown KeyType = iota
	KeyDown
	KeyUp
	KeyChar
)

// Event flags, matching cef_event_flags_t.
const (
	FlagCapsLock    uint32 = 1 << 0
	FlagShift       uint32 = 1 << 1
	FlagControl     uint32 = 1 << 2
	FlagAlt         uint32 = 1 << 3
	FlagLeftButton  uint32 = 1 << 4
	FlagMidButton   uint32 = 1 << 5
	FlagRightButton uint32 = 1 << 6
	FlagCommand     uint32 = 1 << 7
	FlagNumLock     uint32 = 1 << 8
	FlagKeyPad      uint32 = 1 << 9
	FlagRepeat      uint32 = 1 << 13
)

// KeyEvent is one keyboard event, as cef_key_event_t wants it.
type KeyEvent struct {
	Type                KeyType
	Modifiers           uint32
	WindowsKeyCode      int
	NativeKeyCode       int
	Character           uint16
	UnmodifiedCharacter uint16
}
