package platform

// SkipPathHealEnv, when set in a child process's environment to any
// non-empty value, tells that process's own FixEnv call to skip
// ensureCLIOnPath (a macOS-only no-op on every other platform). `tubeless
// config` execs cmd/tubeless-config as a child (see cmd/tubeless's
// runConfigTUI) after the parent has already run FixEnv/ensureCLIOnPath
// itself — the child sets this on its exec.Cmd so it doesn't repeat the
// exact same filesystem writes, and, on failure, doesn't print the exact
// same warning twice for what is really one failure. Declared here (not in
// env_darwin.go) so cmd/tubeless can reference it regardless of GOOS.
const SkipPathHealEnv = "TUBELESS_PATH_FIXED"
