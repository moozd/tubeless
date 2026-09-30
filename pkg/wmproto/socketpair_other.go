//go:build !linux

package wmproto

import (
	"fmt"
	"os"
)

// NewSocketpair is unavailable off Linux: GUI apps in panes need the
// Linux-only tubeless-wm compositor.
func NewSocketpair() (*Conn, *os.File, error) {
	return nil, nil, fmt.Errorf("wmproto: GUI apps in panes are Linux only")
}
