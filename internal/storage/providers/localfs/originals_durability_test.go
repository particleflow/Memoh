package localfs

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/media"
)

func TestUploadRetryRepairsCanonicalRenameBeforeDirectorySync(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	p := NewOriginals(root)
	svc := media.NewService(nil, p)
	svc.SetRequirePersistence(true)
	if err := svc.EnableUploads(root); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Uploads().Close() }()
	data := []byte("canonical rename survived, directory barrier did not")
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	u, err := svc.Uploads().Create(ctx, "owner", "bot", media.UploadInput{SizeBytes: int64(len(data)), SHA256: hash, Mime: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Uploads().Append(ctx, "owner", "bot", u.ID, 0, int64(len(data)), hash, bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	failed := errors.New("injected canonical directory fsync failure")
	barriers := 0
	p.syncFile = func(f *os.File) error {
		info, err := f.Stat()
		if err != nil {
			return err
		}
		if info.IsDir() && filepath.Base(f.Name()) == hash[:2] {
			barriers++
			return failed
		}
		return f.Sync()
	}
	if _, err := svc.Uploads().Complete(ctx, "owner", "bot", u.ID); !errors.Is(err, failed) {
		t.Fatalf("first completion: %v", err)
	}
	original := filepath.Join(root, "bot", hash[:2], hash+".txt")
	if _, err := os.Stat(original); err != nil {
		t.Fatal("rename did not happen", err)
	}
	staging := filepath.Join(root, ".uploads", u.ID+".data")
	// Open succeeds and verifies SHA, but the repeated completion must still fail
	// until the canonical directory barrier succeeds. It must not remove staging.
	if _, err := svc.Uploads().Complete(ctx, "owner", "bot", u.ID); !errors.Is(err, failed) {
		t.Fatalf("dedup skipped durability repair: %v", err)
	}
	if barriers < 2 {
		t.Fatal("retry did not repeat canonical barrier")
	}
	status, err := svc.Uploads().Get(ctx, "owner", "bot", u.ID)
	if err != nil || status.State != "uploading" || status.ContentHash != "" {
		t.Fatal(status, err)
	}
	if _, err := os.Stat(staging); err != nil {
		t.Fatal("lost last staging copy", err)
	}
	p.syncFile = (*os.File).Sync
	ready, err := svc.Uploads().Complete(ctx, "owner", "bot", u.ID)
	if err != nil || ready.State != "ready" {
		t.Fatal(ready, err)
	}
	if _, err := os.Stat(staging); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if _, err := os.Stat(original); err != nil {
		t.Fatal(err)
	}
}

func TestUploadProcessDeathAfterCanonicalRename(t *testing.T) {
	ctx := t.Context()
	data := []byte("canonical publication crash")
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	if root := os.Getenv("MEMOH_CANONICAL_RENAME_CHILD"); root != "" {
		p := NewOriginals(root)
		svc := media.NewService(nil, p)
		svc.SetRequirePersistence(true)
		if err := svc.EnableUploads(root); err != nil {
			t.Fatal(err)
		}
		u, err := svc.Uploads().Create(ctx, "owner", "bot", media.UploadInput{SizeBytes: int64(len(data)), SHA256: hash, Mime: "text/plain"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Uploads().Append(ctx, "owner", "bot", u.ID, 0, int64(len(data)), hash, bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
		p.syncFile = func(f *os.File) error {
			info, err := f.Stat()
			if err != nil {
				return err
			}
			if info.IsDir() && filepath.Base(f.Name()) == hash[:2] {
				_, _ = os.Stdout.WriteString(u.ID + "\n")
				os.Exit(42)
			}
			return f.Sync()
		}
		_, _ = svc.Uploads().Complete(ctx, "owner", "bot", u.ID)
		t.Fatal("did not reach crash boundary")
	}
	root := t.TempDir()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestUploadProcessDeathAfterCanonicalRename$") //nolint:gosec // G204: this test binary and a fixed test selector.
	cmd.Env = append(os.Environ(), "MEMOH_CANONICAL_RENAME_CHILD="+root)
	output, err := cmd.Output()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 42 {
		t.Fatal(string(output), err)
	}
	id := strings.TrimSpace(string(output))
	p := NewOriginals(root)
	barriers := 0
	p.syncFile = func(f *os.File) error {
		info, err := f.Stat()
		if err != nil {
			return err
		}
		if info.IsDir() && filepath.Base(f.Name()) == hash[:2] {
			barriers++
		}
		return f.Sync()
	}
	svc := media.NewService(nil, p)
	svc.SetRequirePersistence(true)
	if err := svc.EnableUploads(root); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Uploads().Close() }()
	ready, err := svc.Uploads().Complete(ctx, "owner", "bot", id)
	if err != nil || ready.State != "ready" || barriers == 0 {
		t.Fatal(ready, err, barriers)
	}
	staging, err := os.OpenRoot(filepath.Join(root, ".uploads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = staging.Close() }()
	if _, err := staging.Stat(id + ".data"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}
