// The app picker `tubeless open` shows when given no command: a
// Spotlight-style fuzzy list of the installed GUI apps (.desktop
// entries), most recently used first.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/term"
)

const recentLimit = 30

// appEntry is one launchable app.
type appEntry struct {
	ID   string // desktop-file id, e.g. "org.gnome.Calculator"
	Name string
	Argv []string
}

func desktopDirs() []string {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			data = filepath.Join(home, ".local", "share")
		}
	}
	dirs := []string{filepath.Join(data, "applications")}
	sys := os.Getenv("XDG_DATA_DIRS")
	if sys == "" {
		sys = "/usr/local/share:/usr/share"
	}
	for _, d := range strings.Split(sys, ":") {
		dirs = append(dirs, filepath.Join(d, "applications"))
	}
	return dirs
}

// loadApps reads every visible application entry; the first entry for an
// id wins, so a user's own file overrides the system one.
func loadApps(dirs []string) []appEntry {
	seen := make(map[string]bool)
	var apps []appEntry
	for _, dir := range dirs {
		walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					fmt.Fprintln(os.Stderr, "tubeless open: scan apps:", err)
				}
				return nil
			}
			if d.IsDir() || !strings.HasSuffix(path, ".desktop") {
				return nil
			}
			rel, _ := filepath.Rel(dir, path)
			id := strings.ReplaceAll(strings.TrimSuffix(rel, ".desktop"), "/", "-")
			if seen[id] {
				return nil
			}
			seen[id] = true
			if e, ok := readDesktopFile(path, id); ok {
				apps = append(apps, e)
			}
			return nil
		})
		if walkErr != nil {
			fmt.Fprintln(os.Stderr, "tubeless open: scan", dir+":", walkErr)
		}
	}
	sort.Slice(apps, func(i, j int) bool { return strings.ToLower(apps[i].Name) < strings.ToLower(apps[j].Name) })
	return apps
}

func readDesktopFile(path, id string) (appEntry, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return appEntry{}, false
	}
	return parseDesktopEntry(string(data), id, os.Getenv("XDG_CURRENT_DESKTOP"))
}

// parseDesktopEntry extracts the [Desktop Entry] group and applies the
// visibility rules: applications only, not hidden, not terminal-only,
// not restricted to another desktop, and runnable.
func parseDesktopEntry(text, id, desktop string) (appEntry, bool) {
	keys := make(map[string]string)
	inEntry := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "["):
			inEntry = line == "[Desktop Entry]"
		case inEntry && strings.Contains(line, "=") && !strings.HasPrefix(line, "#"):
			k, v, _ := strings.Cut(line, "=")
			keys[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	if keys["Type"] != "Application" || keys["NoDisplay"] == "true" || keys["Hidden"] == "true" || keys["Terminal"] == "true" {
		return appEntry{}, false
	}
	if only := keys["OnlyShowIn"]; only != "" && !containsField(only, desktop) {
		return appEntry{}, false
	}
	if try := keys["TryExec"]; try != "" {
		if _, err := exec.LookPath(try); err != nil {
			return appEntry{}, false
		}
	}
	argv := splitExec(keys["Exec"])
	if keys["Name"] == "" || len(argv) == 0 {
		return appEntry{}, false
	}
	return appEntry{ID: id, Name: keys["Name"], Argv: argv}, true
}

func containsField(list, want string) bool {
	for _, f := range strings.Split(list, ";") {
		if f != "" && strings.EqualFold(f, want) {
			return true
		}
	}
	return false
}

// splitExec splits an Exec line into arguments: double quotes group,
// backslash escapes inside them, and the %f/%u-style field codes (which
// would name files to open) are dropped; %% stays a literal percent.
func splitExec(exec string) []string {
	var args []string
	var cur strings.Builder
	quoted, have := false, false
	rs := []rune(exec)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '\\' && quoted && i+1 < len(rs):
			i++
			cur.WriteRune(rs[i])
		case r == '"':
			quoted, have = !quoted, true
		case r == '%' && i+1 < len(rs):
			i++
			if rs[i] == '%' {
				cur.WriteRune('%')
				have = true
			}
		case unicode.IsSpace(r) && !quoted:
			if have {
				args = append(args, cur.String())
				cur.Reset()
				have = false
			}
		default:
			cur.WriteRune(r)
			have = true
		}
	}
	if have {
		args = append(args, cur.String())
	}
	return args
}

// fuzzyScore ranks name against query: -1 when the query's letters do not
// appear in order, higher for consecutive letters, word starts and an
// early first match.
func fuzzyScore(query, name string) int {
	q, n := []rune(strings.ToLower(query)), []rune(strings.ToLower(name))
	score, qi, prev := 0, 0, -2
	for i, r := range n {
		if qi >= len(q) || r != q[qi] {
			continue
		}
		score += 1
		if i == prev+1 {
			score += 5
		}
		if i == 0 {
			score += 10
		} else if !unicode.IsLetter(n[i-1]) && !unicode.IsDigit(n[i-1]) {
			score += 8
		}
		prev = i
		qi++
	}
	if qi < len(q) {
		return -1
	}
	return score - len(n)/8
}

// rankApps filters apps by query and orders them: best fuzzy score first,
// recently used apps breaking ties. An empty query lists recents first,
// then everything alphabetically.
func rankApps(apps []appEntry, query string, recent []string) []appEntry {
	type scored struct {
		app   appEntry
		score int
	}
	recency := make(map[string]int, len(recent))
	for i, id := range recent {
		recency[id] = recentLimit - i
	}
	var out []scored
	for _, a := range apps {
		s := 0
		if query != "" {
			if s = fuzzyScore(query, a.Name); s < 0 {
				continue
			}
			s *= 100
		}
		out = append(out, scored{a, s + recency[a.ID]})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	ranked := make([]appEntry, len(out))
	for i, s := range out {
		ranked[i] = s.app
	}
	return ranked
}

func recentFilePath() string {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "tubeless", "open-recent")
}

func loadRecent(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Fields(string(data))
}

func saveRecent(path, id string, recent []string) error {
	list := []string{id}
	for _, r := range recent {
		if r != id && len(list) < recentLimit {
			list = append(list, r)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create recents dir: %w", err)
	}
	return os.WriteFile(path, []byte(strings.Join(list, "\n")+"\n"), 0o600)
}

// pickApp runs the picker on the current terminal. It returns the chosen
// app's argv, or nil when the user cancels.
func pickApp(stdin <-chan []byte) ([]string, error) {
	apps := loadApps(desktopDirs())
	if len(apps) == 0 {
		return nil, fmt.Errorf("no applications found in %s", strings.Join(desktopDirs(), ", "))
	}
	fd := int(os.Stdin.Fd())
	saved, err := term.MakeRaw(fd)
	if err != nil {
		return nil, fmt.Errorf("raw mode: %w", err)
	}
	fmt.Fprint(os.Stdout, "\x1b[?1049h")
	chosen := runPicker(fd, stdin, apps, loadRecent(recentFilePath()))
	fmt.Fprint(os.Stdout, "\x1b[?25h\x1b[?1049l")
	if err := term.Restore(fd, saved); err != nil {
		return nil, fmt.Errorf("restore terminal: %w", err)
	}
	if chosen == nil {
		return nil, nil
	}
	if err := saveRecent(recentFilePath(), chosen.ID, loadRecent(recentFilePath())); err != nil {
		fmt.Fprintln(os.Stderr, "tubeless open: save recents:", err)
	}
	return chosen.Argv, nil
}

type pickerState struct {
	query   []rune
	sel     int
	matches []appEntry
}

func runPicker(fd int, stdin <-chan []byte, apps []appEntry, recent []string) *appEntry {
	st := &pickerState{}
	st.matches = rankApps(apps, "", recent)
	var dec keyDecoder
	for {
		drawPicker(fd, st)
		b, ok := <-stdin
		if !ok {
			return nil
		}
		events := append(dec.Feed(b), dec.Flush()...)
		for _, ev := range events {
			switch done, pick := st.handle(ev, apps, recent); {
			case pick != nil:
				return pick
			case done:
				return nil
			}
		}
	}
}

// handle applies one key; done=true cancels, a non-nil pick confirms.
func (st *pickerState) handle(ev inputEvent, apps []appEntry, recent []string) (done bool, pick *appEntry) {
	ctrl := ev.mods&modCtrl != 0
	switch {
	case ev.focus:
		return false, nil
	case ev.keysym == keysymEscape, ctrl && (ev.keysym == 'c' || ev.keysym == 'g'):
		return true, nil
	case ev.keysym == keysymReturn:
		if len(st.matches) == 0 {
			return false, nil
		}
		return false, &st.matches[st.sel]
	case ev.keysym == keysymUp, ctrl && ev.keysym == 'p':
		st.sel = max(st.sel-1, 0)
	case ev.keysym == keysymDown, ctrl && ev.keysym == 'n':
		st.sel = min(st.sel+1, max(len(st.matches)-1, 0))
	case ev.keysym == keysymBackSpace:
		st.query = st.query[:max(len(st.query)-1, 0)]
	case ctrl && ev.keysym == 'u':
		st.query = nil
	case ev.mods&^modShift == 0 && printableKeysym(ev.keysym):
		st.query = append(st.query, keysymRune(ev.keysym))
	default:
		return false, nil
	}
	st.matches = rankApps(apps, string(st.query), recent)
	st.sel = min(st.sel, max(len(st.matches)-1, 0))
	return false, nil
}

func printableKeysym(sym uint32) bool {
	return (sym >= 0x20 && sym < 0x7f) || (sym >= 0xa0 && sym <= 0xff) || sym >= 0x01000000
}

func keysymRune(sym uint32) rune {
	if sym >= 0x01000000 {
		return rune(sym - 0x01000000)
	}
	return rune(sym)
}

func drawPicker(fd int, st *pickerState) {
	cols, rows, err := term.GetSize(fd)
	if err != nil {
		cols, rows = 80, 24
	}
	w := bufio.NewWriter(os.Stdout)
	fmt.Fprint(w, "\x1b[?25l\x1b[H\x1b[2J")
	fmt.Fprintf(w, "\x1b[1mopen>\x1b[0m %s\r\n", string(st.query))
	first := max(st.sel-(rows-3), 0)
	for i := first; i < len(st.matches) && i-first < rows-2; i++ {
		name := truncate(st.matches[i].Name, cols-3)
		if i == st.sel {
			fmt.Fprintf(w, "\x1b[7m > %s\x1b[0m\r\n", name)
		} else {
			fmt.Fprintf(w, "   %s\r\n", name)
		}
	}
	fmt.Fprintf(w, "\x1b[1;%dH\x1b[?25h", 7+len(st.query))
	w.Flush()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if n < 1 || len(r) <= n {
		return s
	}
	return string(r[:n])
}
