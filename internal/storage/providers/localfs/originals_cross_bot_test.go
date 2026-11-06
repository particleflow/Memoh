package localfs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"testing"
)

// A hash reference needs to answer "which Bot's directory backs these bytes"
// without knowing the Bot up front. That is the address half of the reference:
// the storage key prefix is Bot-independent and every Bot root is a candidate.
func TestOriginalsListPrefixAcrossBotsFindsOwner(t *testing.T) {
	t.Parallel()

	provider := NewOriginals(t.TempDir())
	ctx := context.Background()
	payload := []byte("cross-bot payload")
	hash := hashOf(payload)
	storageKey := path.Join(hash[:2], hash+".txt")
	hashPrefix := path.Join(hash[:2], hash)
	if err := provider.Put(ctx, path.Join("bot-owner", storageKey), bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}

	keys, err := provider.ListPrefixAcrossBots(ctx, hashPrefix)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != path.Join("bot-owner", storageKey) {
		t.Fatalf("ListPrefixAcrossBots() = %v, want [%s]", keys, path.Join("bot-owner", storageKey))
	}

	absent := hashOf([]byte("absent"))
	missing, err := provider.ListPrefixAcrossBots(ctx, path.Join(absent[:2], absent))
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("ListPrefixAcrossBots() for absent hash = %v, want none", missing)
	}
}

// Several Bot directories may already hold the same bytes (history created
// them before the reference model). The listing stays deterministic and
// metadata-only: Bot name order decides, and no bytes move.
func TestOriginalsListPrefixAcrossBotsDeterministicOrder(t *testing.T) {
	t.Parallel()

	provider := NewOriginals(t.TempDir())
	ctx := context.Background()
	payload := []byte("duplicated payload")
	hash := hashOf(payload)
	storageKey := path.Join(hash[:2], hash+".bin")
	hashPrefix := path.Join(hash[:2], hash)
	for _, bot := range []string{"bot-zeta", "bot-alpha", "bot-mid"} {
		if err := provider.Put(ctx, path.Join(bot, storageKey), bytes.NewReader(payload)); err != nil {
			t.Fatal(err)
		}
	}

	keys, err := provider.ListPrefixAcrossBots(ctx, hashPrefix)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{path.Join("bot-alpha", storageKey), path.Join("bot-mid", storageKey), path.Join("bot-zeta", storageKey)}
	if len(keys) != len(want) {
		t.Fatalf("ListPrefixAcrossBots() = %v, want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("ListPrefixAcrossBots()[%d] = %q, want %q", i, keys[i], want[i])
		}
	}
}

// Non-directory entries and traversal-shaped names in the originals root are
// not Bot directories and must not be listed into.
func TestOriginalsListPrefixAcrossBotsSkipsNonDirectories(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	provider := NewOriginals(root)
	ctx := context.Background()
	payload := []byte("skip probe payload")
	hash := hashOf(payload)
	storageKey := path.Join(hash[:2], hash+".txt")
	hashPrefix := path.Join(hash[:2], hash)
	if err := provider.Put(ctx, path.Join("bot-owner", storageKey), bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "not-a-bot.txt"), []byte("stray"), 0o600); err != nil {
		t.Fatal(err)
	}

	keys, err := provider.ListPrefixAcrossBots(ctx, hashPrefix)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != path.Join("bot-owner", storageKey) {
		t.Fatalf("ListPrefixAcrossBots() = %v, want only the Bot directory key", keys)
	}
}

// A missing originals root answers not-found rather than failing the whole
// reference resolution, matching the per-Bot listing contract.
func TestOriginalsListPrefixAcrossBotsMissingRoot(t *testing.T) {
	t.Parallel()

	provider := NewOriginals(filepath.Join(t.TempDir(), "absent"))
	keys, err := provider.ListPrefixAcrossBots(context.Background(), "aa/aaaa")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ListPrefixAcrossBots() error = %v, want fs.ErrNotExist or empty", err)
	}
	if len(keys) != 0 {
		t.Fatalf("ListPrefixAcrossBots() = %v, want none", keys)
	}
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
