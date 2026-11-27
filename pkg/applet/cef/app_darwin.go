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

// checkApplication stops Init before CEF aborts the process for an
// NSApp that GLFW or something else made first.
func checkApplication() error {
	if C.tb_app_is_ready() == 0 {
		return errors.New("cef: NSApp does not conform to CefAppProtocol (run cef.PrepareApplication before glfw.Init)")
	}
	return nil
}
