package upgrade

import (
	"fmt"
	"os/exec"
	"strings"
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
// The command is run to completion rather than just Start()ed: `open`
// exits as soon as it has handed the launch to LaunchServices, and its
// exit status is the only signal that the relaunch actually happened.
// Start() alone reports success for anything `open` itself rejects — a
// path that isn't an app, a bundle that fails its signature check — which
// is how a failed relaunch could print "Restarting..." and then silently
// do nothing.
func restart(execPath string) error {
	bundleRoot, ok := bundleRootFor(execPath)
	if !ok {
		return fmt.Errorf("relaunch %s: not inside a .app bundle", execPath)
	}
	out, err := exec.Command("open", "-n", "-a", bundleRoot).CombinedOutput()
	if err != nil {
		return fmt.Errorf("open %s: %w: %s", bundleRoot, err, strings.TrimSpace(string(out)))
	}
	return nil
}
