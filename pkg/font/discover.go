package font

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// SystemFamilies lists installed font family names via fontconfig's
// fc-list, deduplicated and sorted — the picker cmd/tubeless-config's
// font.family setting searches. Returns an error if fontconfig isn't
// available (e.g. fc-list not installed); callers should treat that as
// "no picker available" rather than fatal, since a family name chosen
// elsewhere can still be stored and resolved later.
//
// macOS doesn't ship fontconfig by default (it's an X11/Linux-ecosystem
// tool; macOS apps normally use CoreText instead), so this returns an
// error there out of the box and the font-family picker has nothing to
// list — ResolveFamily/loadFontBytes already fall back to the bundled
// font on any error here, so nothing breaks, but the picker convenience
// is unavailable on macOS until a CoreText-based implementation exists.
// That's an intentionally out-of-scope follow-up, not something this
// function works around — see the project's macOS support notes.
func SystemFamilies() ([]string, error) {
	out, err := exec.Command("fc-list", ":", "family").Output()
	if err != nil {
		return nil, fmt.Errorf("fc-list: %w", err)
	}
	seen := make(map[string]bool)
	var names []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		// Each line can list several comma-separated aliases (localized
		// names) for one family; split and dedupe across the whole list.
		for _, part := range strings.Split(sc.Text(), ",") {
			name := strings.TrimSpace(part)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// ResolveFamily finds the font file fontconfig picks for family, via
// fc-match — what cmd/tubeless loads instead of a raw path when
// config.Font.Family is set. fc-match always returns *some* file (it
// falls back to its own default match rather than erroring on an unknown
// family), so a typo reads as "wrong font", never a crash.
func ResolveFamily(family string) (string, error) {
	out, err := exec.Command("fc-match", "--format=%{file}", family).Output()
	if err != nil {
		return "", fmt.Errorf("fc-match %q: %w", family, err)
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", fmt.Errorf("fc-match %q: no file returned", family)
	}
	return path, nil
}
