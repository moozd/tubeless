package vtparse

func (p *Parser) advanceCSI(b byte) {
	if b == 0x1b {
		p.st = stateEscape
		return
	}
	final, ok := p.collectHeader(b)
	if !ok {
		return
	}
	p.sink.CSIDispatch(final, p.params, p.subs, p.intermediates, p.private)
	p.reset()
}
