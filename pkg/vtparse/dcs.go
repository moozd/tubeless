package vtparse

func (p *Parser) advanceDCS(b byte) {
	if b == 0x1b {
		p.reset()
		p.st = stateEscape
		return
	}
	final, ok := p.collectHeader(b)
	if !ok {
		return
	}
	p.sink.DCSStart(final, p.params, p.intermediates, p.private)
	p.st = stateDCSPassthrough
}

func (p *Parser) advanceDCSPassthrough(b byte) {
	if p.pendingST {
		p.pendingST = false
		if b == 0x5c {
			p.sink.DCSEnd()
			p.reset()
			return
		}
		p.sink.DCSEnd()
		p.reset()
		p.Advance(b)
		return
	}
	if b == 0x1b {
		p.pendingST = true
		return
	}
	p.sink.DCSPut(b)
}
