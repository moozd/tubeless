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

// ResolveStyle reports whether family truly has the given style (e.g.
// "Bold", "Italic", "Bold Italic") — unlike ResolveFamily/fc-match, which
// always substitutes *some* file even for a style the family doesn't
// carry (a plain family match, or its own generic fallback), so it can't
// tell "no italic cut exists" apart from "resolved fine". fc-list only
// lists files that actually declare the requested style, so an empty
// result here means the style genuinely isn't available and callers
// should fall back to something else (a synthetic slant, a different
// weight) rather than trusting a substituted file.
func ResolveStyle(family, style string) (path string, ok bool) {
	out, err := exec.Command("fc-list", family+":style="+style, "file").Output()
	if err != nil {
		return "", false
	}
	first, _, _ := strings.Cut(string(out), "\n")
	first = strings.TrimSuffix(strings.TrimSpace(first), ":")
	if first == "" {
		return "", false
	}
	return first, true
}

// ResolveFamily finds the font file fontconfig picks for family, via
// fc-match — what cmd/tubeless loads instead of a raw path when
// config.Font.Family is set. fc-match never reports "not found": if
// fontconfig doesn't know family (not installed, or installed but not
// in a directory fontconfig indexes — common on macOS, where fontconfig
// itself is usually missing too), it silently substitutes its own
// default match instead of erroring. Left unchecked, that reads as
// "resolved fine" and the caller renders with the wrong font's glyph
// set — so this also asks fc-match which family it actually picked and
// rejects the match unless it corresponds to what was requested,
// turning a silent wrong-font substitution into the same "wrong font"
// fallback a typo already gets.
func ResolveFamily(family string) (string, error) {
	out, err := exec.Command("fc-match", "--format=%{file}", family).Output()
	if err != nil {
		return "", fmt.Errorf("fc-match %q: %w", family, err)
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", fmt.Errorf("fc-match %q: no file returned", family)
	}

	famOut, err := exec.Command("fc-match", "--format=%{family}", family).Output()
	if err != nil {
		return "", fmt.Errorf("fc-match %q: %w", family, err)
	}
	got := strings.TrimSpace(string(famOut))
	if !hasFamilyAlias(got, family) {
		return "", fmt.Errorf("fc-match %q: no installed font matches (fontconfig substituted %q)", family, got)
	}
	return path, nil
}

// hasFamilyAlias reports whether want matches any of got's comma-separated
// family aliases (fontconfig lists localized names alongside the primary
// one), case-insensitively.
func hasFamilyAlias(got, want string) bool {
	for _, alias := range strings.Split(got, ",") {
		if strings.EqualFold(strings.TrimSpace(alias), want) {
			return true
		}
	}
	return false
}
