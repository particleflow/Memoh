package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// Inventory only this synthetic fixture. Stream hashes instead of keeping
// payloads in memory; callers compare both membership and original contents.
func runtimeWakeFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			files[path] = "directory"
			return nil
		}
		f, err := os.Open(path) //nolint:gosec // Enumerated synthetic fixture only.
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		stat, err := f.Stat()
		if err != nil {
			return err
		}
		hash := sha256.New()
		if _, err := io.Copy(hash, f); err != nil {
			return err
		}
		files[path] = hex.EncodeToString(hash.Sum(nil)) + ":" + strconv.FormatInt(stat.Size(), 10) + ":" + stat.ModTime().UTC().Format(time.RFC3339Nano)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
