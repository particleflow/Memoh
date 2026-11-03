package fallback

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

// A Bot reading another Bot's original by reference needs the owner's verified
// bytes refreshed into its own addressable workspace, without the canonical
// store gaining a second owner of those bytes.
func TestDurableMaterializeAccessPathWritesConsumerCopyOnly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	payload := []byte("owner original bytes")
	hash := fmt.Sprintf("%x", sha256.Sum256(payload))
	ownerKey := "bot-owner/" + hash[:2] + "/" + hash + ".txt"
	consumerKey := "bot-reader/" + hash[:2] + "/" + hash + ".txt"
	root := t.TempDir()
	workspace := newMemoryProvider("/data/.memoh/media")
	provider := NewDurable(workspace, localfs.NewOriginals(root), newMemoryProvider(""))
	if err := provider.Put(ctx, ownerKey, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}

	got, err := provider.MaterializeAccessPath(ctx, consumerKey, ownerKey)
	if err != nil {
		t.Fatal(err)
	}
	if want := "/data/.memoh/media/" + consumerKey; got != want {
		t.Fatalf("MaterializeAccessPath() = %q, want %q", got, want)
	}
	if !bytes.Equal(workspace.values[consumerKey], payload) {
		t.Fatalf("consumer copy = %q, want owner bytes", workspace.values[consumerKey])
	}
	// The owner keeps the only canonical copy: no reader directory appears.
	if _, err := os.Stat(filepath.Join(root, "bot-reader")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("consumer materialization wrote canonical storage: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(ownerKey))); err != nil {
		t.Fatalf("owner original disturbed: %v", err)
	}
}

// Materializing refreshes from the owner's verified original, so a stale or
// tool-edited consumer copy does not survive a re-read by reference.
func TestDurableMaterializeAccessPathRefreshesFromOriginal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	payload := []byte("immutable by reference")
	hash := fmt.Sprintf("%x", sha256.Sum256(payload))
	ownerKey := "bot-owner/" + hash[:2] + "/" + hash + ".txt"
	consumerKey := "bot-reader/" + hash[:2] + "/" + hash + ".txt"
	workspace := newMemoryProvider("/data/.memoh/media")
	provider := NewDurable(workspace, localfs.NewOriginals(t.TempDir()), newMemoryProvider(""))
	if err := provider.Put(ctx, ownerKey, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.MaterializeAccessPath(ctx, consumerKey, ownerKey); err != nil {
		t.Fatal(err)
	}
	workspace.values[consumerKey] = []byte("consumer tool edited the copy")
	if _, err := provider.MaterializeAccessPath(ctx, consumerKey, ownerKey); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(workspace.values[consumerKey], payload) {
		t.Fatal("re-materialization kept an edited copy instead of refreshing")
	}
}

func TestDurableMaterializeAccessPathRejectsMissingSource(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace := newMemoryProvider("/data/.memoh/media")
	provider := NewDurable(workspace, localfs.NewOriginals(t.TempDir()), newMemoryProvider(""))
	if _, err := provider.MaterializeAccessPath(ctx, "bot-reader/aa/aaaa.txt", "bot-owner/aa/aaaa.txt"); err == nil {
		t.Fatal("materialized bytes without a source original")
	}
	if len(workspace.values) != 0 {
		t.Fatal("failed materialization left workspace bytes")
	}
}

// The cross-Bot listing resolves preserved originals without contacting the
// workspace, mirroring the per-Bot ListPrefix contract.
func TestDurableListPrefixAcrossBotsPrefersCanonicalOriginals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	payload := []byte("address resolution payload")
	hash := fmt.Sprintf("%x", sha256.Sum256(payload))
	storageKey := hash[:2] + "/" + hash + ".txt"
	hashPrefix := hash[:2] + "/" + hash
	workspace := newMemoryProvider("")
	root := t.TempDir()
	spill := newMemoryProvider("")
	provider := NewDurable(workspace, localfs.NewOriginals(root), spill)
	if err := provider.Put(ctx, "bot-owner/"+storageKey, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	spill.values["bot-legacy/"+storageKey] = payload

	keys, err := provider.ListPrefixAcrossBots(ctx, hashPrefix)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != "bot-owner/"+storageKey {
		t.Fatalf("ListPrefixAcrossBots() = %v, want the canonical owner only", keys)
	}
}
