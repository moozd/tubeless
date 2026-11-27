package applet

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/evanw/esbuild/pkg/api"
)

// entryNames are the files an applet may start from, in order.
var entryNames = []string{"main.ts", "main.tsx", "main.js", "main.jsx"}

// errNoEntry means the applet folder has no main.ts or main.js.
var errNoEntry = errors.New("no main.ts, main.tsx, main.js or main.jsx")

// bundle is one esbuild result: its output files by URL path, and the
// mtime of every input it read, to tell when it is out of date.
type bundle struct {
	files  map[string][]byte
	inputs map[string]time.Time
}

// bundler builds applets with esbuild and keeps each result until one
// of its inputs changes. TypeScript, JSX and npm imports from the
// applet's own node_modules all work with no build step of the user's.
type bundler struct {
	mu    sync.Mutex
	cache map[string]*bundle
}

func newBundler() *bundler {
	return &bundler{cache: make(map[string]*bundle)}
}

// file returns output file urlPath (such as "/main.js") of the applet
// in dir, building it first if needed.
func (b *bundler) file(dir, urlPath string) ([]byte, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	cached := b.cache[dir]
	if cached == nil || cached.isStale(dir) {
		built, err := buildApplet(dir)
		if err != nil {
			return nil, false, err
		}
		b.cache[dir] = built
		cached = built
	}
	data, ok := cached.files[urlPath]
	return data, ok, nil
}

func (bd *bundle) isStale(dir string) bool {
	for name, mtime := range bd.inputs {
		st, err := os.Stat(filepath.Join(dir, name))
		if err != nil || !st.ModTime().Equal(mtime) {
			return true
		}
	}
	return false
}

func findEntry(dir string) (string, error) {
	for _, name := range entryNames {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return name, nil
		}
	}
	return "", errNoEntry
}

func buildApplet(dir string) (*bundle, error) {
	entry, err := findEntry(dir)
	if err != nil {
		return nil, err
	}
	result := api.Build(api.BuildOptions{
		EntryPoints:   []string{entry},
		AbsWorkingDir: dir,
		Bundle:        true,
		Write:         false,
		Outdir:        "/out",
		EntryNames:    "main",
		AssetNames:    "assets/[name]-[hash]",
		PublicPath:    "/",
		Format:        api.FormatESModule,
		Platform:      api.PlatformBrowser,
		Target:        api.ES2022,
		JSX:           api.JSXAutomatic,
		// Packages such as Excalidraw export their CSS only under the
		// production or development condition.
		Conditions:   []string{"production"},
		Metafile:     true,
		LogLevel:     api.LogLevelSilent,
		MinifySyntax: true,
		// React and most npm UI code read NODE_ENV to drop dev checks.
		Define: map[string]string{"process.env.NODE_ENV": `"production"`},
		Loader: map[string]api.Loader{
			".woff": api.LoaderFile, ".woff2": api.LoaderFile,
			".ttf": api.LoaderFile, ".png": api.LoaderFile,
			".svg": api.LoaderFile, ".wasm": api.LoaderFile,
		},
	})
	if len(result.Errors) > 0 {
		return nil, buildError(result.Errors)
	}
	bd := &bundle{files: make(map[string][]byte)}
	for _, f := range result.OutputFiles {
		rel, err := filepath.Rel("/out", f.Path)
		if err != nil {
			return nil, fmt.Errorf("bundle output %s: %w", f.Path, err)
		}
		bd.files["/"+filepath.ToSlash(rel)] = f.Contents
	}
	bd.inputs, err = inputTimes(dir, result.Metafile)
	if err != nil {
		return nil, err
	}
	return bd, nil
}

func buildError(msgs []api.Message) error {
	lines := make([]string, 0, len(msgs))
	for _, m := range msgs {
		where := ""
		if m.Location != nil {
			where = fmt.Sprintf("%s:%d:%d: ", m.Location.File, m.Location.Line, m.Location.Column)
		}
		lines = append(lines, where+m.Text)
	}
	return fmt.Errorf("build failed:\n%s", strings.Join(lines, "\n"))
}

func inputTimes(dir, metafile string) (map[string]time.Time, error) {
	var meta struct {
		Inputs map[string]json.RawMessage `json:"inputs"`
	}
	if err := json.Unmarshal([]byte(metafile), &meta); err != nil {
		return nil, fmt.Errorf("read esbuild metafile: %w", err)
	}
	times := make(map[string]time.Time, len(meta.Inputs))
	for name := range meta.Inputs {
		if strings.Contains(name, ":") || path.IsAbs(name) {
			continue
		}
		st, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("stat bundle input: %w", err)
		}
		times[name] = st.ModTime()
	}
	return times, nil
}
