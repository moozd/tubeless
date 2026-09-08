package vtparse

import "unicode/utf8"

// utf8Lead reports how many bytes a UTF-8 sequence starting with b needs,
// or 0 if b isn't a valid lead byte (treated as Latin-1 passthrough).
func utf8Lead(b byte) int {
	switch {
	case b&0x80 == 0x00:
		return 1
	case b&0xE0 == 0xC0:
		return 2
	case b&0xF0 == 0xE0:
		return 3
	case b&0xF8 == 0xF0:
		return 4
	default:
		return 0
	}
}

func (p *Parser) advanceGround(b byte) {
	if len(p.utf8Buf) > 0 {
		p.continueUTF8(b)
		return
	}
	if b == 0x1b {
		p.st = stateEscape
		return
	}
	if b < 0x20 || b == 0x7f {
		p.sink.Execute(b)
		return
	}
	if n := utf8Lead(b); n > 1 {
		p.utf8Buf = append(p.utf8Buf[:0], b)
		p.utf8Need = n
		return
	}
	p.sink.Print(rune(b))
}

func (p *Parser) continueUTF8(b byte) {
	p.utf8Buf = append(p.utf8Buf, b)
	if len(p.utf8Buf) < p.utf8Need {
		return
	}
	r, _ := utf8.DecodeRune(p.utf8Buf)
	p.sink.Print(r)
	p.utf8Buf = p.utf8Buf[:0]
}
