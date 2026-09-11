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
// -n forces a genuinely new process: `tubeless upgrade` is very often run
// from a shell inside an already-open tubeless window, and without -n,
// LaunchServices treats that still-running (pre-upgrade) instance as
// "the app already running" and just refocuses it instead of starting
// the newly-installed binary — printing "Restarting..." and then
// appearing to do nothing.
func restart(execPath string) error {
	bundleRoot := bundleRootFor(execPath)
	if err := exec.Command("open", "-n", "-a", bundleRoot).Start(); err != nil {
		return fmt.Errorf("open %s: %w", bundleRoot, err)
	}
	return nil
}
