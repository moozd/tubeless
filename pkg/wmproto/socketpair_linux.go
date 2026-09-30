package wmproto

import (
	"fmt"
	"os"
	"syscall"
)

// NewSocketpair returns the window's end and the compositor's end (as a
// file to pass through exec.Cmd.ExtraFiles, where it becomes fd 3).
func NewSocketpair() (*Conn, *os.File, error) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_SEQPACKET|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("wmproto: socketpair: %w", err)
	}
	mine := os.NewFile(uintptr(fds[0]), "wm-window")
	theirs := os.NewFile(uintptr(fds[1]), "wm-compositor")
	conn, err := NewConn(mine)
	mine.Close()
	if err != nil {
		theirs.Close()
		return nil, nil, err
	}
	return conn, theirs, nil
}
