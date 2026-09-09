package vtparse

// collectHeader handles the byte range shared by CSI and DCS sequence
// headers: an optional private marker, ';'-separated numeric params
// (each optionally followed by its own ':'-separated sub-parameters, e.g.
// curly underline's "4:3" or an underline color's "58:2::r:g:b"), and
// intermediate bytes. It reports the final dispatch byte once seen.
func (p *Parser) collectHeader(b byte) (final byte, isFinal bool) {
	switch {
	case b >= '0' && b <= '9':
		p.curParam = p.curParam*10 + int(b-'0')
		p.haveParam = true
	case b == ':':
		if !p.haveColon {
			p.curTop = p.curParam
			p.haveColon = true
		} else {
			p.curSub = append(p.curSub, p.curParam)
		}
		p.curParam = 0
		p.haveParam = false
	case b == ';':
		p.pushParam()
	case b >= 0x3c && b <= 0x3f: // private marker: < = > ?
		p.private = b
	case b >= 0x20 && b <= 0x2f:
		p.intermediates = append(p.intermediates, b)
	case b >= 0x40 && b <= 0x7e:
		p.pushParam()
		return b, true
	}
	return 0, false
}

func (p *Parser) pushParam() {
	if p.haveColon {
		p.params = append(p.params, p.curTop)
		p.subs = append(p.subs, append(p.curSub, p.curParam))
		p.curTop, p.haveColon, p.curSub = 0, false, nil
		p.curParam, p.haveParam = 0, false
		return
	}
	if !p.haveParam && len(p.params) == 0 {
		return
	}
	p.params = append(p.params, p.curParam)
	p.subs = append(p.subs, nil)
	p.curParam = 0
	p.haveParam = false
}
