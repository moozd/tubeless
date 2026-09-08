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
	CSIDispatch(final byte, params []int, intermediates []byte, private byte)
	EscDispatch(final byte, intermediates []byte)
	OSCDispatch(data []byte)
	DCSStart(final byte, params []int, intermediates []byte, private byte)
	DCSPut(b byte)
	DCSEnd()
}

// Parser is a byte-at-a-time state machine per the VT500 parser model
// (DEC STD 070 / vt100.net), extended with a private-marker byte for CSI
// and DCS so callers can distinguish e.g. "CSI ?" from plain "CSI".
type Parser struct {
	st            state
	sink          Sink
	params        []int
	curParam      int
	haveParam     bool
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
	p.curParam = 0
	p.haveParam = false
	p.intermediates = p.intermediates[:0]
	p.private = 0
}
