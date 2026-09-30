package ptyio

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// TTY is the path of the PTY's slave end (e.g. /dev/pts/3): what a
// process running inside the session reports as its terminal.
func (s *Session) TTY() string {
	n, err := unix.IoctlGetInt(int(s.Master.Fd()), unix.TIOCGPTN)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("/dev/pts/%d", n)
}
