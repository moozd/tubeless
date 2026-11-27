package main

import (
	"testing"

	"github.com/go-gl/glfw/v3.4/glfw"

	"github.com/moozd/tubeless/pkg/applet/cef"
)

func TestBundleOf(t *testing.T) {
	got, err := bundleOf("/Users/me/Applications/Tubeless.app/Contents/MacOS/tubeless")
	if err != nil || got != "/Users/me/Applications/Tubeless.app" {
		t.Fatalf("bundleOf = %q, %v", got, err)
	}
	for _, exe := range []string{"/repo/bin/tubeless", "/x/Contents/MacOS/tubeless"} {
		if got, err := bundleOf(exe); err == nil {
			t.Errorf("bundleOf(%q) = %q, want an error", exe, got)
		}
	}
}

func TestCefOptionsFromRuntimeDir(t *testing.T) {
	t.Setenv("TUBELESS_CEF_DIR", "/A/Tubeless.app/Contents/Frameworks")
	o, err := cefOptions()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"LibPath":        "/A/Tubeless.app/Contents/Frameworks/Chromium Embedded Framework.framework/Chromium Embedded Framework",
		"SubprocessPath": "/A/Tubeless.app/Contents/Frameworks/tubeless Helper.app/Contents/MacOS/tubeless Helper",
		"MainBundle":     "/A/Tubeless.app",
	}
	got := map[string]string{"LibPath": o.LibPath, "SubprocessPath": o.SubprocessPath, "MainBundle": o.MainBundle}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestEditCommand(t *testing.T) {
	cases := []struct {
		key    glfw.Key
		mods   glfw.ModifierKey
		want   cef.EditCommand
		isEdit bool
	}{
		{glfw.KeyC, glfw.ModSuper, cef.EditCopy, true},
		{glfw.KeyV, glfw.ModSuper | glfw.ModCapsLock, cef.EditPaste, true},
		{glfw.KeyZ, glfw.ModSuper | glfw.ModShift, cef.EditRedo, true},
		{glfw.KeyC, glfw.ModControl, 0, false},
		{glfw.KeyQ, glfw.ModSuper, 0, false},
	}
	for _, c := range cases {
		got, isEdit := editCommand(c.key, c.mods)
		if isEdit != c.isEdit || got != c.want {
			t.Errorf("editCommand(%v, %v) = %v, %v; want %v, %v", c.key, c.mods, got, isEdit, c.want, c.isEdit)
		}
	}
}
