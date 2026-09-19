package main

import (
	"runtime"

	"github.com/go-gl/glfw/v3.4/glfw"

	"github.com/moozd/tubeless/pkg/render"
)

// fullscreenState remembers the windowed position/size to restore when
// leaving fullscreen — GLFW's own GetMonitor reports whether the window
// is currently fullscreen but not what geometry it had beforehand, so
// this is the only record of what "windowed" should snap back to.
type fullscreenState struct {
	x, y, w, h int
}

// toggleFullscreen switches win between fullscreen (borderless, sized to
// whichever monitor the window currently sits on) and its saved
// windowed position/size, via GLFW's SetMonitor — the same call backs
// native fullscreen on macOS, Windows, and Linux (X11 and Wayland
// alike), so no per-OS branch is needed here.
func toggleFullscreen(win *render.Window, fs *fullscreenState) {
	if win.GetMonitor() != nil {
		win.SetMonitor(nil, fs.x, fs.y, fs.w, fs.h, 0)
		return
	}
	fs.x, fs.y = win.GetPos()
	fs.w, fs.h = win.GetSize()

	mon := win.CurrentMonitor()
	if mon == nil {
		mon = glfw.GetPrimaryMonitor()
	}
	mode := mon.GetVideoMode()
	win.SetMonitor(mon, 0, 0, mode.Width, mode.Height, mode.RefreshRate)
}

// isFullscreenShortcut reports whether key+mods is this platform's
// toggle-fullscreen binding: Ctrl+Cmd+F on macOS (the system convention
// every native macOS app answers to), F11 elsewhere (the binding shared
// by virtually every Linux/Windows app and browser).
func isFullscreenShortcut(key glfw.Key, mods glfw.ModifierKey) bool {
	if runtime.GOOS == "darwin" {
		return key == glfw.KeyF && mods&glfw.ModControl != 0 && mods&glfw.ModSuper != 0
	}
	return key == glfw.KeyF11
}
