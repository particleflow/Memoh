//go:build linux || darwin

package transferclient

import (
	"os"
	"path/filepath"
	"testing"
)

func configFixtureRoot(t *testing.T) string {
	t.Helper()
	// testing.TempDir's numbered child uses 0777 masked by the caller's umask.
	// Create our fixture with explicit privacy, independent of runner policy.
	path := filepath.Join(t.TempDir(), "private-config")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
