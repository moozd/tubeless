//go:build !darwin

package platform

// FixEnv is a no-op outside macOS: Linux desktop sessions (and anything
// launching tubeless from a shell) already have a complete PATH by the
// time the process starts, unlike a macOS app launched from Finder/Dock.
func FixEnv() {}
