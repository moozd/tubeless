package screen

// Charset identifies which G-set a byte stream position designates into.
type Charset int

const (
	CharsetASCII Charset = iota
	CharsetDECSpecial
)

// decSpecialGraphics maps the DEC Special Graphics charset's printable
// range (0x60-0x7e) onto the closest Unicode glyphs, per DEC STD 070.
var decSpecialGraphics = map[rune]rune{
	'`': '◆', 'a': '▒', 'b': '␉', 'c': '␌', 'd': '␍', 'e': '␊',
	'f': '°', 'g': '±', 'h': '␤', 'i': '␋', 'j': '┘', 'k': '┐',
	'l': '┌', 'm': '└', 'n': '┼', 'o': '⎺', 'p': '⎻', 'q': '─',
	'r': '⎼', 's': '⎽', 't': '├', 'u': '┤', 'v': '┴', 'w': '┬',
	'x': '│', 'y': '≤', 'z': '≥', '{': 'π', '|': '≠', '}': '£',
	'~': '·',
}

func translateRune(cs Charset, r rune) rune {
	if cs != CharsetDECSpecial {
		return r
	}
	if mapped, ok := decSpecialGraphics[r]; ok {
		return mapped
	}
	return r
}
