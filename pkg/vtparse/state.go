// Package vtparse implements a hand-rolled VT340-oriented escape sequence
// state machine: C0 controls, ESC sequences, CSI, OSC, and DCS. DCS is the
// seam where sixel/ReGIS payloads get diverted before falling through to
// plain text handling.
package vtparse

type state int

const (
	stateGround state = iota
	stateEscape
	stateCSI
	stateOSC
	stateDCS
	stateDCSPassthrough
)

// Sink receives the parser's decoded actions. Screen mutation, sixel, and
// ReGIS handlers all implement this against their own backing state.
type Sink interface {
	Print(r rune)
	Execute(b byte)
	CSIDispatch(final byte, params []int, subs [][]int, intermediates []byte, private byte)
	EscDispatch(final byte, intermediates []byte)
	OSCDispatch(data []byte)
	DCSStart(final byte, params []int, subs [][]int, intermediates []byte, private byte)
	DCSPut(b byte)
	DCSEnd()
}

// Parser is a byte-at-a-time state machine per the VT500 parser model
// (DEC STD 070 / vt100.net), extended with a private-marker byte for CSI
// and DCS so callers can distinguish e.g. "CSI ?" from plain "CSI".
type Parser struct {
	st        state
	sink      Sink
	params    []int
	curParam  int
	haveParam bool
	// subs holds, for each entry in params, whatever ':'-separated
	// sub-parameters followed it (nil if none) — e.g. ECMA-48 style curly
	// underline (CSI 4:3m) or an underline color (CSI 58:2::r:g:bm). curTop
	// is the value before the first ':' for the param currently being
	// collected, curSub accumulates the values after it — see collectHeader
	// and pushParam.
	subs          [][]int
	curTop        int
	haveColon     bool
	curSub        []int
	intermediates []byte
	private       byte
	oscBuf        []byte
	utf8Buf       []byte
	utf8Need      int
	pendingST     bool
}

func New(sink Sink) *Parser {
	return &Parser{sink: sink}
}

func (p *Parser) Advance(b byte) {
	switch p.st {
	case stateGround:
		p.advanceGround(b)
	case stateEscape:
		p.advanceEscape(b)
	case stateCSI:
		p.advanceCSI(b)
	case stateOSC:
		p.advanceOSC(b)
	case stateDCS:
		p.advanceDCS(b)
	case stateDCSPassthrough:
		p.advanceDCSPassthrough(b)
	}
}

func (p *Parser) Write(data []byte) {
	for _, b := range data {
		p.Advance(b)
	}
}

func (p *Parser) reset() {
	p.st = stateGround
	p.params = p.params[:0]
	p.subs = p.subs[:0]
	p.curParam = 0
	p.haveParam = false
	p.curTop = 0
	p.haveColon = false
	p.curSub = nil
	p.intermediates = p.intermediates[:0]
	p.private = 0
}
