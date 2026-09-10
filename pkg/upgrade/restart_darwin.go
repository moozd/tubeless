package upgrade

import (
	"fmt"
	"os/exec"
)

// restart relaunches the upgraded app. execPath is
// .../Tubeless.app/Contents/MacOS/tubeless; `open -a` (rather than
// exec'ing the binary directly) is the canonical way to launch a macOS
// app bundle — it hands the process off to launchd/LaunchServices
// properly detached from this one, the way Finder or the Dock would.
func restart(execPath string) error {
	bundleRoot := bundleRootFor(execPath)
	if err := exec.Command("open", "-a", bundleRoot).Start(); err != nil {
		return fmt.Errorf("open %s: %w", bundleRoot, err)
	}
	return nil
}
