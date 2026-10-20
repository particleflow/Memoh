package localfs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func originalKey(bot string, payload []byte) string {
	hash := fmt.Sprintf("%x", sha256.Sum256(payload))
	return bot + "/" + hash[:2] + "/" + hash + ".bin"
}

type brokenReader struct{ cancel context.CancelFunc }

func (r brokenReader) Read(b []byte) (int, error) {
	if r.cancel != nil {
		r.cancel()
	}
	copy(b, "partial")
	return len("partial"), errors.New("synthetic interrupted upload")
}

func TestOriginalsAtomicIntegrityAndCancellation(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	disk, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = disk.Close() }()
	p := NewOriginals(root)
	payload := []byte("the complete original")
	key := originalKey("bot-a", payload)
	if err := p.Put(ctx, key, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	for name, reader := range map[string]io.Reader{
		"failure": brokenReader{}, "wrong hash": strings.NewReader("different bytes"),
	} {
		t.Run(name, func(t *testing.T) {
			if err := p.Put(ctx, key, reader); err == nil {
				t.Fatal("partial or mismatched write succeeded")
			}
			got, err := disk.ReadFile(key)
			if err != nil || !bytes.Equal(got, payload) {
				t.Fatalf("original replaced: %q, %v", got, err)
			}
		})
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	if err := p.Put(cancelCtx, originalKey("bot-b", payload), brokenReader{cancel}); err == nil {
		t.Fatal("canceled upload succeeded")
	}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && strings.HasPrefix(d.Name(), ".pending-") {
			t.Errorf("temporary upload remains: %s", p)
		}
		return err
	})
	if _, err := p.Open(ctx, originalKey("bot-b", payload)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled original visible: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, key), []byte("disk corruption"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r, err := p.Open(ctx, key); err == nil {
		_ = r.Close()
		t.Fatal("corrupted original was served")
	}
}

func TestOriginalsConfinesEveryBotAndRejectsTraversal(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	disk, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = disk.Close() }()
	p := NewOriginals(root)
	payload := []byte("bot b private original")
	key := originalKey("bot-b", payload)
	if err := p.Put(ctx, key, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"../" + key, "/" + key, "bot-a/../" + key, "bot-a\\..\\" + key} {
		if err := p.Put(ctx, key, bytes.NewReader(payload)); err == nil {
			t.Errorf("accepted unsafe key %q", key)
		}
		if r, err := p.Open(ctx, key); err == nil {
			_ = r.Close()
			t.Errorf("opened unsafe key %q", key)
		}
	}
	if err := os.Symlink("bot-b", filepath.Join(root, "bot-link")); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Open(ctx, strings.Replace(key, "bot-b/", "bot-link/", 1)); err == nil {
		t.Fatal("bot directory symlink crossed bot boundary")
	}
	if err := os.Mkdir(filepath.Join(root, "bot-a"), 0o750); err != nil {
		t.Fatal(err)
	}
	sub := strings.TrimPrefix(key, "bot-b/")
	prefix := strings.Split(sub, "/")[0]
	if err := os.Symlink("../bot-b/"+prefix, filepath.Join(root, "bot-a", prefix)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Open(ctx, "bot-a/"+sub); err == nil {
		t.Fatal("nested symlink crossed bot boundary")
	}
	if err := p.Put(ctx, "bot-a/"+sub, bytes.NewReader(payload)); err == nil {
		t.Fatal("write followed symlink out of bot root")
	}
	if err := p.Delete(ctx, "bot-a/"+sub); err == nil {
		t.Fatal("delete followed symlink out of bot root")
	}
	other, err := disk.ReadFile(key)
	if err != nil || !bytes.Equal(other, payload) {
		t.Fatalf("other bot original changed: %v", err)
	}
}
