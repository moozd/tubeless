package applet

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// Access is how much of the file system an applet may read or write.
type Access string

const (
	// AccessNone allows nothing.
	AccessNone Access = "none"
	// AccessArgs allows only the files named in the applet's args.
	AccessArgs Access = "args"
	// AccessAll allows every file the user can reach.
	AccessAll Access = "all"
)

// Manifest is an applet's optional applet.toml.
type Manifest struct {
	Title       string      `toml:"title"`
	Permissions Permissions `toml:"permissions"`
}

// Permissions limit what the bridge does for an applet.
type Permissions struct {
	Read  Access `toml:"read"`
	Write Access `toml:"write"`
}

// LoadManifest reads dir/applet.toml. A missing file gives the
// defaults: read the files named in args, write nothing.
func LoadManifest(dir string) (Manifest, error) {
	m := Manifest{Permissions: Permissions{Read: AccessArgs, Write: AccessNone}}
	data, err := os.ReadFile(filepath.Join(dir, "applet.toml"))
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return m, fmt.Errorf("read applet.toml: %w", err)
	}
	if err := toml.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("parse %s: %w", filepath.Join(dir, "applet.toml"), err)
	}
	if !m.Permissions.Read.isValid() || !m.Permissions.Write.isValid() {
		return m, fmt.Errorf("applet.toml: read and write must be none, args or all")
	}
	return m, nil
}

func (a Access) isValid() bool {
	switch a {
	case AccessNone, AccessArgs, AccessAll:
		return true
	}
	return false
}
