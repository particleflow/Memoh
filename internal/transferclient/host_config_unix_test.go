//go:build linux || darwin

package transferclient

import "testing"

func configFixtureRoot(t *testing.T) string { t.Helper(); return t.TempDir() }
