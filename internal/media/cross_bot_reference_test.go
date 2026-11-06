package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/storage"
	"github.com/felinics/memoh/internal/storage/providers/fallback"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

// crossBotFixture builds the deployed storage shape: disposable workspace
// copies plus one canonical originals root holding every Bot's own bytes.
type crossBotFixture struct {
	service    *Service
	root       string
	workspace  map[string][]byte
	payload    []byte
	hash       string
	storageKey string
}

func newCrossBotFixture(t *testing.T, ownerBot string) *crossBotFixture {
	t.Helper()
	root := t.TempDir()
	workspace := newCrossBotMemoryProvider()
	provider := fallback.NewDurable(workspace, localfs.NewOriginals(root), newCrossBotMemoryProvider())
	service := NewService(nil, provider)
	service.SetRequirePersistence(true)
	payload := []byte("owned by one Bot, read by reference")
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	asset, err := service.Ingest(context.Background(), IngestInput{BotID: ownerBot, Mime: "image/png", Reader: bytes.NewReader(payload)})
	if err != nil {
		t.Fatal(err)
	}
	return &crossBotFixture{
		service:    service,
		root:       root,
		workspace:  workspace.values,
		payload:    payload,
		hash:       hash,
		storageKey: asset.StorageKey,
	}
}

// G3: any Bot session holding a content hash reference reads the one original
// wherever it lives. The read is a view: no second Bot directory appears.
func TestOpenResolvesReferenceToOwnerOriginal(t *testing.T) {
	t.Parallel()

	f := newCrossBotFixture(t, "bot-owner")
	ctx := context.Background()

	reader, asset, err := f.service.Open(ctx, "bot-reader", f.hash)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || !bytes.Equal(got, f.payload) {
		t.Fatalf("referenced read = %q, %v; want owner bytes", got, err)
	}
	if asset.BotID != "bot-owner" || asset.ContentHash != f.hash {
		t.Fatalf("reference resolved to %+v; want the owner's asset", asset)
	}
	if _, err := os.Stat(filepath.Join(f.root, "bot-reader")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reference read materialized canonical bytes: %v", err)
	}
}

// Metadata lookups follow the same reference resolution as byte reads.
func TestStatAndResolveFollowReferenceToOwner(t *testing.T) {
	t.Parallel()

	f := newCrossBotFixture(t, "bot-owner")
	ctx := context.Background()

	asset, err := f.service.Stat(ctx, "bot-reader", f.hash)
	if err != nil || asset.BotID != "bot-owner" || asset.Mime != "image/png" {
		t.Fatalf("Stat() = %+v, %v; want owner metadata", asset, err)
	}
	resolved, err := f.service.Resolve(ctx, "bot-reader", f.hash)
	if err != nil || resolved.StorageKey != f.storageKey || resolved.BotID != "bot-owner" {
		t.Fatalf("Resolve() = %+v, %v; want owner asset", resolved, err)
	}
	if _, err := f.service.Stat(ctx, "bot-reader", strings.Repeat("a", 64)); !errors.Is(err, ErrAssetNotFound) {
		t.Fatalf("absent hash Stat() = %v; want ErrAssetNotFound", err)
	}
}

// G1: the path tools consume container paths. A reference materializes the
// owner's verified bytes as the reading Bot's disposable access copy; the
// canonical store still has exactly one owner directory.
func TestEnsureAccessPathForMaterializesConsumerCopy(t *testing.T) {
	t.Parallel()

	f := newCrossBotFixture(t, "bot-owner")
	ctx := context.Background()

	ownerAsset, err := f.service.Resolve(ctx, "bot-owner", f.hash)
	if err != nil {
		t.Fatal(err)
	}
	path, err := f.service.EnsureAccessPathFor(ctx, "bot-reader", ownerAsset)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, "bot-reader") || !strings.Contains(path, f.storageKey) {
		t.Fatalf("consumer path = %q; want the reading Bot's addressable copy", path)
	}
	if !bytes.Equal(f.workspace["bot-reader/"+f.storageKey], f.payload) {
		t.Fatal("consumer copy missing owner bytes")
	}
	if _, err := os.Stat(filepath.Join(f.root, "bot-reader")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("access copy wrote canonical bytes: %v", err)
	}

	// The owner's own path keeps the existing materialization semantics.
	ownPath, err := f.service.EnsureAccessPathFor(ctx, "bot-owner", ownerAsset)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ownPath, "bot-owner") {
		t.Fatalf("owner path = %q; want the owner's addressable copy", ownPath)
	}
}

// Verified ranges keep the named Bot's scope: this surface backs HTTP range
// delivery and task input production, where the request's Bot is the owner the
// bytes must come from. A hash the Bot does not own stays not-found here even
// though the reference route can read it.
func TestOpenSeekableKeepsBotScope(t *testing.T) {
	t.Parallel()

	f := newCrossBotFixture(t, "bot-owner")
	ctx := context.Background()

	if _, _, err := f.service.OpenSeekable(ctx, "bot-reader", f.hash); !errors.Is(err, ErrAssetNotFound) {
		t.Fatalf("OpenSeekable(foreign) = %v; want ErrAssetNotFound", err)
	}
	reader, asset, err := f.service.OpenSeekable(ctx, "bot-owner", f.hash)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	if asset.BotID != "bot-owner" {
		t.Fatalf("OpenSeekable() asset = %+v; want owner", asset)
	}
	got, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(got, f.payload) {
		t.Fatalf("OpenSeekable() bytes = %q, %v; want owner payload", got, err)
	}
}

// A hash nobody owns anywhere answers the same not-found error the requester's
// own scoped store already returned.
func TestReferenceResolutionKeepsNotFoundSemantics(t *testing.T) {
	t.Parallel()

	f := newCrossBotFixture(t, "bot-owner")
	ctx := context.Background()
	absent := strings.Repeat("b", 64)
	if _, _, err := f.service.Open(ctx, "bot-reader", absent); !errors.Is(err, ErrAssetNotFound) {
		t.Fatalf("Open(absent) = %v; want ErrAssetNotFound", err)
	}
	if _, err := f.service.EnsureAccessPathFor(ctx, "bot-reader", Asset{BotID: "bot-owner", ContentHash: absent, StorageKey: "bb/" + absent + ".png"}); !errors.Is(err, storage.ErrAccessPathUnavailable) && !errors.Is(err, ErrAssetNotFound) {
		t.Fatalf("EnsureAccessPathFor(absent) = %v; want unavailable/not-found", err)
	}
}

// Malformed hashes never reach storage on the reference path.
func TestReferenceResolutionRejectsMalformedHash(t *testing.T) {
	t.Parallel()

	provider := &originalLookupProbe{err: errors.New("storage must not be reached")}
	service := NewService(nil, provider)
	service.SetRequirePersistence(true)
	for _, hash := range []string{"ab", "not-a-hash", strings.Repeat("A", 64), "../private"} {
		if _, _, err := service.Open(context.Background(), "bot-a", hash); !errors.Is(err, ErrAssetNotFound) {
			t.Fatalf("Open(%q) = %v; want ErrAssetNotFound", hash, err)
		}
	}
}

// crossBotMemoryProvider is a minimal in-memory workspace for materialization
// probes; it exposes container-style paths.
type crossBotMemoryProvider struct {
	values map[string][]byte
}

func newCrossBotMemoryProvider() *crossBotMemoryProvider {
	return &crossBotMemoryProvider{values: map[string][]byte{}}
}

func (p *crossBotMemoryProvider) Put(_ context.Context, key string, reader io.Reader) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	p.values[key] = data
	return nil
}

func (p *crossBotMemoryProvider) Open(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := p.values[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (*crossBotMemoryProvider) Delete(context.Context, string) error { return nil }

func (*crossBotMemoryProvider) AccessPath(_ context.Context, key string) string {
	return "/data/.memoh/media/" + key
}
