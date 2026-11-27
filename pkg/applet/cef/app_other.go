//go:build cef && !darwin

package cef

// PrepareApplication does nothing: only macOS needs a special NSApp.
func PrepareApplication() {}

func checkApplication() error { return nil }
