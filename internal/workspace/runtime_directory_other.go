//go:build !linux

package workspace

// The NAS fix uses Linux descriptor-relative, directory-only removal. Preserve
// the existing behavior on other platforms rather than use a path-based fallback
// that could follow a replaced parent or remove a file in a race.
func (*Manager) cleanupBotRunDirectory(string) error { return nil }
