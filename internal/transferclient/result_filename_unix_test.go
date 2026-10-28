//go:build linux || darwin

package transferclient

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExplicitFilenameProtectedOpen(t *testing.T) {
	for _, boundary := range []string{"symlink", "hardlink", "revoked", "traversal", "stream"} {
		t.Run(boundary, func(t *testing.T) {
			if boundary == "revoked" && os.Geteuid() == 0 {
				t.Skip("chmod denial needs an ordinary identity; no identity substitution")
			}
			getenv, _, write := producerFixture(t, "https://fixture.invalid")
			write("source", "photo.png", []byte("isolated source bytes"))
			cfg, err := OpenHostConfig(getenv)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cfg.Close() }()
			if f, err := cfg.OpenSource("photo.png"); err != nil {
				t.Fatal("explicit source name rejected", err)
			} else {
				_ = f.Close()
			}
			path, ref := filepath.Join(cfg.sourcePath, "photo.png"), "photo.png"
			switch boundary {
			case "symlink":
				if err := os.Rename(path, path+"-original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+"-original", path); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(path, filepath.Join(cfg.sourcePath, "alias.png")); err != nil {
					t.Fatal(err)
				}
			case "revoked":
				if err := os.Chmod(path, 0); err != nil {
					t.Fatal(err)
				}
				defer func() { _ = os.Chmod(path, 0o600) }()
			case "traversal":
				ref = "../photo.png"
			case "stream":
				ref = "photo.png:stream"
			}
			if f, err := cfg.OpenSource(ref); err == nil {
				_ = f.Close()
				t.Fatal("protected source boundary relaxed", boundary)
			}
		})
	}
}
