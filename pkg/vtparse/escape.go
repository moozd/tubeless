package vtparse

func (p *Parser) advanceEscape(b byte) {
	switch {
	case b == 0x5b: // '[' -> CSI
		p.beginCSI()
	case b == 0x5d: // ']' -> OSC
		p.st = stateOSC
		p.oscBuf = p.oscBuf[:0]
	case b == 0x50: // 'P' -> DCS
		p.beginCSI()
		p.st = stateDCS
	case b >= 0x20 && b <= 0x2f: // intermediate bytes
		p.intermediates = append(p.intermediates, b)
	case b >= 0x30 && b <= 0x7e: // final byte
		p.sink.EscDispatch(b, p.intermediates)
		p.reset()
	default:
		p.reset()
	}
}

func (p *Parser) beginCSI() {
	p.st = stateCSI
	p.params = p.params[:0]
	p.curParam = 0
	p.haveParam = false
	p.intermediates = p.intermediates[:0]
	p.private = 0
}
