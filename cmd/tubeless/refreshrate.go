package main

import (
	"github.com/go-gl/glfw/v3.4/glfw"

	"github.com/moozd/tubeless/pkg/render"
)

// refreshRate caches the refresh rate of the monitor the window is on.
// frameBudget used to query it every frame: a scan of every monitor and
// a display mode query 60 times a second. While a display sleeps each
// failed query logs a GLFW PlatformError ("Cocoa: Failed to query
// display mode"), 90k lines in 3 hours on 2026-10-28. The cache only
// queries again after the window moves or a monitor connects or
// disconnects. Both callbacks and get run on the locked main thread.
type refreshRate struct {
	hz      int
	isStale bool
}

// watchRefreshRate starts at 60Hz and marks the cache stale on every
// window move and monitor change.
func watchRefreshRate(win *render.Window) *refreshRate {
	r := &refreshRate{hz: 60, isStale: true}
	win.SetPosCallback(func(_ *glfw.Window, _, _ int) {
		r.isStale = true
	})
	glfw.SetMonitorCallback(func(_ *glfw.Monitor, _ glfw.PeripheralEvent) {
		r.isStale = true
	})
	return r
}

// get reports the cached rate, querying the monitor first when stale.
// A failed query keeps the last known rate.
func (r *refreshRate) get(win *render.Window) int {
	if !r.isStale {
		return r.hz
	}
	r.isStale = false
	m := win.CurrentMonitor()
	if m == nil {
		return r.hz
	}
	if mode := m.GetVideoMode(); mode != nil && mode.RefreshRate > 0 {
		r.hz = mode.RefreshRate
	}
	return r.hz
}
