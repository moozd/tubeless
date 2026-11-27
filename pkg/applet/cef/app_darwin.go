//go:build cef

package cef

/*
#cgo LDFLAGS: -framework Cocoa
#include "app_darwin.h"
*/
import "C"

import "errors"

// PrepareApplication makes NSApp an application CEF accepts. Run it on
// the main thread before glfw.Init, which keeps the NSApp it finds.
func PrepareApplication() { C.tb_app_prepare() }

// CurrentKeyChars gives the characters of the key event AppKit is
// handling at this time, as CEF wants them on macOS: CEF makes its own
// NSEvent from them, and takes a key event with no characters for a
// modifier change. Use it in the GLFW key callback. ok is false when
// no key event is being handled.
func CurrentKeyChars() (chars, unmodified uint16, ok bool) {
	var c, u C.ushort
	if C.tb_current_key_chars(&c, &u) == 0 {
		return 0, 0, false
	}
	return uint16(c), uint16(u), true
}

// checkApplication stops Init before CEF aborts the process for an
// NSApp that GLFW or something else made first.
func checkApplication() error {
	if C.tb_app_is_ready() == 0 {
		return errors.New("cef: NSApp does not conform to CefAppProtocol (run cef.PrepareApplication before glfw.Init)")
	}
	return nil
}
