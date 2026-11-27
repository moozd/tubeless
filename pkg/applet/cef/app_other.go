//go:build cef && !darwin

package cef

// PrepareApplication does nothing: only macOS needs a special NSApp.
func PrepareApplication() {}

// CurrentKeyChars has nothing to give: key events carry their
// characters only on macOS.
func CurrentKeyChars() (chars, unmodified uint16, ok bool) { return 0, 0, false }

func checkApplication() error { return nil }
