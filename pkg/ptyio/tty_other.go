//go:build !linux

package ptyio

// TTY is only needed to find a window from inside tmux for GUI apps in
// panes, which is Linux only.
func (s *Session) TTY() string {
	return ""
}
