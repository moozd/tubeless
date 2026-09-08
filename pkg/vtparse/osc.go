package vtparse

func (p *Parser) advanceOSC(b byte) {
	if p.pendingST {
		p.pendingST = false
		if b == 0x5c {
			p.dispatchOSC()
			return
		}
		p.dispatchOSC()
		p.Advance(b)
		return
	}
	switch b {
	case 0x07: // BEL terminates OSC
		p.dispatchOSC()
	case 0x1b:
		p.pendingST = true
	default:
		p.oscBuf = append(p.oscBuf, b)
	}
}

func (p *Parser) dispatchOSC() {
	p.sink.OSCDispatch(p.oscBuf)
	p.reset()
}
