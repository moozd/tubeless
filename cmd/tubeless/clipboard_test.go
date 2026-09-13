package main

import (
	"testing"
)

type fakeClipboard struct {
	text string
}

func (f *fakeClipboard) GetClipboardString() string  { return f.text }
func (f *fakeClipboard) SetClipboardString(s string) { f.text = s }

func TestReadClipboardPrefersX11(t *testing.T) {
	origBridge, origRead := bridgeX11, xclipRead
	defer func() { bridgeX11, xclipRead = origBridge, origRead }()

	bridgeX11 = func() bool { return true }
	xclipRead = func() (string, bool) { return "from-x11", true }

	win := &fakeClipboard{text: "stale-wayland"}
	if got := readClipboard(win); got != "from-x11" {
		t.Errorf("readClipboard = %q, want %q", got, "from-x11")
	}
}

func TestReadClipboardFallsBackToWayland(t *testing.T) {
	origBridge, origRead := bridgeX11, xclipRead
	defer func() { bridgeX11, xclipRead = origBridge, origRead }()

	bridgeX11 = func() bool { return true }
	xclipRead = func() (string, bool) { return "", false }

	win := &fakeClipboard{text: "wayland"}
	if got := readClipboard(win); got != "wayland" {
		t.Errorf("readClipboard = %q, want %q", got, "wayland")
	}
}

func TestReadClipboardNoBridge(t *testing.T) {
	origBridge := bridgeX11
	defer func() { bridgeX11 = origBridge }()

	bridgeX11 = func() bool { return false }
	win := &fakeClipboard{text: "wayland"}
	if got := readClipboard(win); got != "wayland" {
		t.Errorf("readClipboard = %q, want %q", got, "wayland")
	}
}

func TestWriteClipboardBoth(t *testing.T) {
	origBridge, origWrite := bridgeX11, xclipWrite
	defer func() { bridgeX11, xclipWrite = origBridge, origWrite }()

	bridgeX11 = func() bool { return true }
	var x11Text string
	xclipWrite = func(s string) { x11Text = s }

	win := &fakeClipboard{}
	writeClipboard(win, "hello")
	if x11Text != "hello" {
		t.Errorf("xclipWrite got %q, want %q", x11Text, "hello")
	}
	if win.text != "hello" {
		t.Errorf("SetClipboardString got %q, want %q", win.text, "hello")
	}
}

func TestWriteClipboardWaylandOnly(t *testing.T) {
	origBridge, origWrite := bridgeX11, xclipWrite
	defer func() { bridgeX11, xclipWrite = origBridge, origWrite }()

	bridgeX11 = func() bool { return false }
	called := false
	xclipWrite = func(string) { called = true }

	win := &fakeClipboard{}
	writeClipboard(win, "hello")
	if called {
		t.Error("xclipWrite should not be called when not bridging")
	}
	if win.text != "hello" {
		t.Errorf("SetClipboardString got %q, want %q", win.text, "hello")
	}
}
