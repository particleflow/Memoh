package fallback

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

type offlineWorkspace struct {
	*memoryProvider
	t *testing.T
}

func (p *offlineWorkspace) Open(context.Context, string) (io.ReadCloser, error) {
	p.t.Fatal("reading a preserved original contacted the workspace")
	return nil, nil
}

func (p *offlineWorkspace) ListPrefix(context.Context, string) ([]string, error) {
	p.t.Fatal("resolving a preserved hash contacted the workspace")
	return nil, nil
}

func TestDurableHashReferenceDoesNotRequireWorkspace(t *testing.T) {
	ctx := context.Background()
	workspace := &offlineWorkspace{newMemoryProvider(""), t}
	provider := NewDurable(workspace, localfs.NewOriginals(t.TempDir()), newMemoryProvider(""))
	service := media.NewService(nil, provider)
	service.SetRequirePersistence(true)
	// Put directly: ingest dedup is allowed to check legacy storage on a miss.
	payload := []byte("preserved file without a workspace")
	hash := fmt.Sprintf("%x", sha256.Sum256(payload))
	key := "bot-a/" + hash[:2] + "/" + hash + ".txt"
	if err := provider.Put(ctx, key, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	reader, asset, err := service.Open(ctx, "bot-a", hash)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	got, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(got, payload) || asset.ContentHash != hash {
		t.Fatalf("read original by hash: %q, %+v, %v", got, asset, err)
	}
}

func TestDurableOriginalSurvivesWorkspaceRecreationAndProviderRestart(t *testing.T) {
	ctx := context.Background()
	payload := []byte("original attachment, not disposable workspace state")
	hash := fmt.Sprintf("%x", sha256.Sum256(payload))
	key := "bot-a/" + hash[:2] + "/" + hash + ".txt"
	root := t.TempDir()
	workspace := newMemoryProvider("/data/.memoh/media")
	spill := newMemoryProvider("")
	p := NewDurable(workspace, localfs.NewOriginals(root), spill)
	if err := p.Put(ctx, key, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.EnsureAccessPath(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, key)); err != nil {
		t.Fatalf("materialization removed original: %v", err)
	}
	workspace.values[key] = []byte("workspace tool edited the copy")
	if _, err := p.EnsureAccessPath(ctx, key); err != nil || !bytes.Equal(workspace.values[key], payload) {
		t.Fatalf("did not refresh from original: %v", err)
	}
	workspace = newMemoryProvider("/data/.memoh/media")
	restarted := NewDurable(workspace, localfs.NewOriginals(root), spill)
	r, err := restarted.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("original after restart: %q, %v", got, err)
	}
	if _, err := restarted.EnsureAccessPath(ctx, key); err != nil || !bytes.Equal(workspace.values[key], payload) {
		t.Fatalf("recreated workspace cannot materialize original: %v", err)
	}
	if _, err := restarted.Open(ctx, "bot-b/"+hash[:2]+"/"+hash+".txt"); err == nil {
		t.Fatal("another bot read original")
	}
}

func TestDurablePreservesLegacyAndFailsClosedOnOriginalWriteFailure(t *testing.T) {
	ctx := context.Background()
	payload := []byte("legacy attachment")
	hash := fmt.Sprintf("%x", sha256.Sum256(payload))
	key := "bot-a/" + hash[:2] + "/" + hash + ".txt"
	workspace, spill := newMemoryProvider("/data"), newMemoryProvider("")
	spill.values[key] = payload
	root := t.TempDir()
	p := NewDurable(workspace, localfs.NewOriginals(root), spill)
	r, err := p.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	if !bytes.Equal(spill.values[key], payload) {
		t.Fatal("legacy source removed")
	}
	badRoot := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(badRoot, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	broken := NewDurable(workspace, localfs.NewOriginals(badRoot), spill)
	if err := broken.Put(ctx, key, bytes.NewReader(payload)); err == nil {
		t.Fatal("acknowledged write without durable original")
	}
	if _, exists := workspace.values[key]; exists {
		t.Fatal("failed write leaked into workspace")
	}
	if _, err := broken.Open(ctx, key); err == nil {
		t.Fatal("served legacy bytes while original storage was unavailable")
	}
}
