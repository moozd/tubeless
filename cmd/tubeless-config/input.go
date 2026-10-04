package main

// feed consumes one input byte. Escape sequences arrive a byte at a time,
// so they accumulate in u.esc until their final byte shows up; a lone
// Escape is resolved by main's timeout (see cancelEscape).
func (u *ui) feed(b byte) (quit bool) {
	if len(u.esc) > 0 {
		return u.feedEscape(b)
	}
	if b == 0x1b {
		u.esc = append(u.esc, b)
		return false
	}
	u.status = ""
	switch {
	case u.picker != nil:
		u.feedPicker(b)
	case u.searching:
		u.feedSearch(b)
	default:
		return u.feedNormal(b)
	}
	return false
}

func (u *ui) feedEscape(b byte) (quit bool) {
	if len(u.esc) == 1 && b != '[' {
		u.cancelEscape()
		return u.feed(b)
	}
	u.esc = append(u.esc, b)
	if len(u.esc) < 3 || b < 0x40 {
		return false
	}
	seq := u.esc
	u.esc = nil
	if len(seq) == 3 {
		u.feedCSI(seq[2])
	}
	return false
}

// cancelEscape handles a bare Escape: close the picker, or leave search.
func (u *ui) cancelEscape() {
	u.esc = nil
	switch {
	case u.picker != nil:
		u.cancelPicker()
	case u.searching || u.query != "":
		u.clearSearch()
	}
}

func (u *ui) feedCSI(final byte) {
	u.status = ""
	if u.picker != nil {
		switch final {
		case 'A':
			u.pickerMove(-1)
		case 'B':
			u.pickerMove(1)
		}
		return
	}
	switch final {
	case 'A':
		u.move(-1)
	case 'B':
		u.move(1)
	case 'C':
		u.adjust(1)
	case 'D':
		u.adjust(-1)
	case 'H':
		u.firstSetting()
	case 'F':
		u.lastSetting()
	case 'Z':
		u.switchCategory(-1)
	}
}

func (u *ui) feedPicker(b byte) {
	if b == '\r' || b == '\n' {
		u.commitPicker()
		return
	}
	u.pickerType(b)
}

func (u *ui) feedSearch(b byte) {
	switch {
	case b == '\r' || b == '\n':
		u.searching = false
	case b == 0x7f || b == '\b':
		if rs := []rune(u.query); len(rs) > 0 {
			u.setQuery(string(rs[:len(rs)-1]))
		}
	case b >= 0x20 && b < 0x7f:
		u.setQuery(u.query + string(rune(b)))
	}
}

func (u *ui) feedNormal(b byte) (quit bool) {
	switch b {
	case 'q', 0x03:
		return true
	case 's':
		u.save()
	case 'r':
		u.resetAxis()
	case '\t':
		u.switchCategory(1)
	case '/':
		u.searching = true
	case '\r', '\n':
		u.openSelectedPicker()
	case 'k':
		u.move(-1)
	case 'j':
		u.move(1)
	case 'l':
		u.adjust(1)
	case 'h':
		u.adjust(-1)
	case 'g':
		u.firstSetting()
	case 'G':
		u.lastSetting()
	}
	return false
}

func (u *ui) openSelectedPicker() {
	if s := u.cur(); s != nil && s.pick != nil {
		u.openPicker(s)
	}
}
