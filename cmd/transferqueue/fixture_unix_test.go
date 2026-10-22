//go:build linux || darwin

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func cliPrivateRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "cli-fixture")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}
