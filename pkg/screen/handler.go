package screen

import (
	"bytes"
	"encoding/base64"

	"github.com/moozd/tubeless/pkg/sixel"
	"github.com/moozd/tubeless/pkg/vtparse"
)

// Handler adapts a Screen to vtparse.Sink, translating parsed VT340 control
// sequences into grid mutations. ReGIS DCS payloads are still stubbed here
// (a future phase); sixel is decoded and placed at the cursor.
type Handler struct {
	*Screen
	sixelDec *sixel.Decoder
}

func NewHandler(s *Screen) *Handler {
	return &Handler{Screen: s}
}

func (h *Handler) Print(r rune) {
	h.Put(r)
}

func (h *Handler) Execute(b byte) {
	switch b {
	case '\b':
		h.MoveBy(-1, 0)
	case '\t':
		h.tab()
	case '\n', '\v', '\f':
		h.LineFeed()
	case '\r':
		h.CarriageReturn()
	case 0x0e: // SO
		h.ShiftOut()
	case 0x0f: // SI
		h.ShiftIn()
	}
}

func (h *Handler) tab() {
	next := (h.CursorX/8 + 1) * 8
	h.MoveTo(min(next, h.Cols-1), h.CursorY)
}

func (h *Handler) EscDispatch(final byte, intermediates []byte) {
	if len(intermediates) == 1 {
		h.dispatchCharsetDesignate(intermediates[0], final)
		return
	}
	switch final {
	case 'D': // IND
		h.LineFeed()
	case 'M': // RI
		h.reverseIndex()
	case 'E': // NEL
		h.CarriageReturn()
		h.LineFeed()
	case 'c': // RIS
		h.reset()
	case '7': // DECSC
		h.saveCursor()
	case '8': // DECRC
		h.restoreCursor()
	}
}

func (h *Handler) saveCursor() {
	h.saved = savedCursor{x: h.CursorX, y: h.CursorY, attr: h.CurAttr}
}

func (h *Handler) restoreCursor() {
	h.CursorX, h.CursorY, h.CurAttr = h.saved.x, h.saved.y, h.saved.attr
}

func (h *Handler) dispatchCharsetDesignate(intermediate, final byte) {
	cs := charsetFromFinal(final)
	if intermediate == '(' {
		h.DesignateG0(cs)
	}
	if intermediate == ')' {
		h.DesignateG1(cs)
	}
}

func charsetFromFinal(final byte) Charset {
	if final == '0' {
		return CharsetDECSpecial
	}
	return CharsetASCII
}

func (h *Handler) reverseIndex() {
	if h.CursorY == h.ScrollTop {
		h.ScrollDown(1)
		return
	}
	h.MoveBy(0, -1)
}

func (h *Handler) reset() {
	h.Screen.Reset()
}

func (h *Handler) OSCDispatch(data []byte) {
	h.dispatchOSC52(data)
}

// dispatchOSC52 handles "52;<selector>;<base64>" — an app asking the
// terminal to set the system clipboard to some text, the portable
// escape-sequence alternative to shelling out to xclip/wl-copy. This is
// how tmux's copy-mode reaches past its own internal paste buffer out to
// the real system clipboard (`set-clipboard on`), and how vim/nvim's
// OSC52 clipboard providers work over SSH or inside tmux/screen — without
// it, those all silently stop at whatever nested session or multiplexer
// they're running under. selector is accepted whatever its value (xterm
// allows combinations like "c", "p", "cp"); tubeless has only the one
// system clipboard to set, so any non-empty selector maps to it. Only
// the "set" direction is handled — a query ("52;c;?") is intentionally
// ignored, since answering it would let any program silently read
// whatever's currently on the user's clipboard.
func (h *Handler) dispatchOSC52(data []byte) {
	parts := bytes.SplitN(data, []byte(";"), 3)
	if len(parts) != 3 || string(parts[0]) != "52" || len(parts[1]) == 0 {
		return
	}
	payload := parts[2]
	if len(payload) == 0 || string(payload) == "?" {
		return
	}
	decoded, err := base64.StdEncoding.DecodeString(string(payload))
	if err != nil {
		return
	}
	h.pendingClipboard = append(h.pendingClipboard, string(decoded))
}

// DCSStart begins collecting a DCS payload. The sixel introducer is
// "DCS P1;P2;P3 q" — final 'q', no intermediates, no private marker — with
// no other DCS use of a bare 'q' final to confuse it with; anything else
// (e.g. a future ReGIS introducer) is left uncollected.
func (h *Handler) DCSStart(final byte, params []int, subs [][]int, intermediates []byte, private byte) {
	if final == 'q' && private == 0 && len(intermediates) == 0 {
		h.sixelDec = sixel.NewDecoder()
	}
}

func (h *Handler) DCSPut(b byte) {
	if h.sixelDec != nil {
		h.sixelDec.Put(b)
	}
}

func (h *Handler) DCSEnd() {
	if h.sixelDec == nil {
		return
	}
	h.Images = append(h.Images, PlacedImage{Col: h.CursorX, Row: h.CursorY, Img: h.sixelDec.Image()})
	h.sixelDec = nil
	h.CarriageReturn()
	h.LineFeed()
}

var _ vtparse.Sink = (*Handler)(nil)
