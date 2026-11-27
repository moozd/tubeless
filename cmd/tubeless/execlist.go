package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"

	"github.com/moozd/tubeless/pkg/applet"
)

// appletEntry is one applet that `tubeless exec --list` shows.
type appletEntry struct {
	name  string
	title string
	// isUser: the applet is in the user's folder, the first search dir.
	isUser bool
	// replaces: a built-in applet with the same name is hidden.
	replaces bool
}

// runExecList prints every applet `tubeless exec` can open.
func runExecList(out io.Writer) error {
	entries, err := listApplets(appletSearchDirs(), os.Stderr)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Fprintf(out, "no applets (put one in %s)\n", appletsDir())
		return nil
	}
	return printApplets(out, entries)
}

// listApplets reads the applets in dirs, sorted by name. As in
// findApplet, an applet in an earlier dir replaces the one with the
// same name in a later dir. A bad applet.toml goes to warn.
func listApplets(dirs []string, warn io.Writer) ([]appletEntry, error) {
	byName := map[string]*appletEntry{}
	for i, dir := range dirs {
		names, err := appletNames(dir)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			addApplet(byName, name, filepath.Join(dir, name), i == 0, warn)
		}
	}
	return sortedApplets(byName), nil
}

// addApplet records the applet in dir, unless an earlier dir had one
// with the same name.
func addApplet(byName map[string]*appletEntry, name, dir string, isUser bool, warn io.Writer) {
	if found, ok := byName[name]; ok {
		found.replaces = found.replaces || (found.isUser && !isUser)
		return
	}
	byName[name] = &appletEntry{name: name, title: appletTitle(name, dir, warn), isUser: isUser}
}

// appletNames lists the applet folders in dir. A missing dir has none.
func appletNames(dir string) ([]string, error) {
	items, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read applets in %s: %w", dir, err)
	}
	var names []string
	for _, item := range items {
		if item.IsDir() {
			names = append(names, item.Name())
		}
	}
	return names, nil
}

// appletTitle is the title in applet.toml, or the name without one.
func appletTitle(name, dir string, warn io.Writer) string {
	m, err := applet.LoadManifest(dir)
	if err != nil {
		fmt.Fprintf(warn, "tubeless exec: applet %s: %v\n", name, err)
		return name
	}
	if m.Title == "" {
		return name
	}
	return m.Title
}

func sortedApplets(byName map[string]*appletEntry) []appletEntry {
	entries := make([]appletEntry, 0, len(byName))
	for _, e := range byName {
		entries = append(entries, *e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	return entries
}

func printApplets(out io.Writer, entries []appletEntry) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tTITLE\tSOURCE")
	for _, e := range entries {
		fmt.Fprintf(w, "%s\t%s\t%s\n", e.name, e.title, e.source())
	}
	return w.Flush()
}

func (e appletEntry) source() string {
	switch {
	case e.replaces:
		return "user (replaces built-in)"
	case e.isUser:
		return "user"
	}
	return "built-in"
}
