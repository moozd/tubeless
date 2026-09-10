package upgrade

import (
	"fmt"
	"os/exec"
	"syscall"
)

// restart relaunches the upgraded binary as a fresh, detached process —
// Setsid so it survives this one exiting once the upgrade command
// returns, the same way launching tubeless from a desktop entry does.
func restart(execPath string) error {
	cmd := exec.Command(execPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", execPath, err)
	}
	return nil
}
