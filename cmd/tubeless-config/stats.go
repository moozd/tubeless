package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// tubeless-config is always launched as a direct child of the tubeless
// binary — either exec'd by `tubeless config` (see cmd/tubeless's
// runConfigTUI) or running as the shell of a live
// `tubeless --shell=tubeless-config` window (see this package's own doc
// comment) — so its parent PID always identifies the tubeless process to
// show live stats for. No IPC needed.

// detectTubelessParent reports the PID of the parent process, if it's
// actually named "tubeless" — a standalone/dev invocation of
// tubeless-config (whose parent is an ordinary shell) reports ok=false so
// the stats pane can degrade to "not running under tubeless" instead of
// monitoring an unrelated process.
func detectTubelessParent() (pid int, ok bool) {
	ppid := os.Getppid()
	out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(ppid)).Output()
	if err != nil {
		return 0, false
	}
	name := strings.TrimSpace(string(out))
	// comm= can report either a bare name or a full path depending on
	// platform (macOS's ps tends toward the full executable path).
	if base := name; strings.Contains(base, "/") {
		base = base[strings.LastIndex(base, "/")+1:]
		name = base
	}
	if name != "tubeless" {
		return 0, false
	}
	return ppid, true
}

// procStats is one sample of a process's live resource usage.
type procStats struct {
	cpuPercent float64
	rssKB      int64
	uptimeSec  float64
}

// sampleProc reads pid's current CPU%, resident memory (KB), and elapsed
// running time (seconds) via `ps` — identical invocation on Linux and
// macOS, avoiding separate /proc parsing and macOS libproc/cgo code paths
// for what is otherwise a very small amount of information.
func sampleProc(pid int) (procStats, error) {
	out, err := exec.Command("ps", "-o", "%cpu=,rss=,etimes=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return procStats{}, err
	}
	fields := strings.Fields(string(out))
	if len(fields) < 3 {
		return procStats{}, errNoProcess
	}
	cpu, _ := strconv.ParseFloat(fields[0], 64)
	rss, _ := strconv.ParseInt(fields[1], 10, 64)
	uptime, _ := strconv.ParseFloat(fields[2], 64)
	return procStats{cpuPercent: cpu, rssKB: rss, uptimeSec: uptime}, nil
}

var errNoProcess = &procError{"process not found"}

type procError struct{ msg string }

func (e *procError) Error() string { return e.msg }

// ring is a small fixed-size history buffer for the stats pane's
// sparklines — push overwrites the oldest sample once full; values
// returns them oldest-first.
type ring struct {
	buf    []float64
	pos    int
	filled bool
}

func newRing(size int) *ring {
	return &ring{buf: make([]float64, size)}
}

func (r *ring) push(v float64) {
	r.buf[r.pos] = v
	r.pos = (r.pos + 1) % len(r.buf)
	if r.pos == 0 {
		r.filled = true
	}
}

// values returns every sample currently held, oldest first.
func (r *ring) values() []float64 {
	if !r.filled {
		return r.buf[:r.pos]
	}
	out := make([]float64, 0, len(r.buf))
	out = append(out, r.buf[r.pos:]...)
	out = append(out, r.buf[:r.pos]...)
	return out
}

// formatUptime renders elapsed seconds as a compact "Xh Ym"/"Xm Ys"/"Xs"
// string, matching the config screen's terse label style.
func formatUptime(sec float64) string {
	total := int64(sec)
	h, rem := total/3600, total%3600
	m, s := rem/60, rem%60
	switch {
	case h > 0:
		return strconv.FormatInt(h, 10) + "h " + strconv.FormatInt(m, 10) + "m"
	case m > 0:
		return strconv.FormatInt(m, 10) + "m " + strconv.FormatInt(s, 10) + "s"
	default:
		return strconv.FormatInt(s, 10) + "s"
	}
}

// formatRSS renders kilobytes as a compact "N MB"/"N.N GB" string.
func formatRSS(kb int64) string {
	const mb = 1024
	if kb >= mb*1024 {
		return strconv.FormatFloat(float64(kb)/(mb*1024), 'f', 1, 64) + " GB"
	}
	return strconv.FormatInt(kb/mb, 10) + " MB"
}
