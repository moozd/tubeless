// tubeless-applet-bundle prebuilds applets for a release package: each
// applet folder (with its npm packages installed) becomes a folder that
// runs with no build step and no node_modules (see applet.Prebuild).
//
//	tubeless-applet-bundle <out-dir> <applet-dir>...
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/moozd/tubeless/pkg/applet"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: tubeless-applet-bundle <out-dir> <applet-dir>...")
		os.Exit(2)
	}
	out := os.Args[1]
	for _, src := range os.Args[2:] {
		dst := filepath.Join(out, filepath.Base(filepath.Clean(src)))
		if err := applet.Prebuild(src, dst); err != nil {
			fmt.Fprintf(os.Stderr, "tubeless-applet-bundle: %s: %v\n", src, err)
			os.Exit(1)
		}
		fmt.Printf("prebuilt %s -> %s\n", src, dst)
	}
}
