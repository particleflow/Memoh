package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/storage"
	"github.com/felinics/memoh/internal/storage/providers/fallback"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

type locatorStoreProbe struct {
	storage.Provider
	opens, puts, lists int
	keys               []string
	err                error
}

func (p *locatorStoreProbe) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	p.opens++
	return p.Provider.Open(ctx, key)
}

func (p *locatorStoreProbe) Put(ctx context.Context, key string, r io.Reader) error {
	p.puts++
	return p.Provider.Put(ctx, key, r)
}

func (p *locatorStoreProbe) ListPrefix(ctx context.Context, prefix string) ([]string, error) {
	p.lists++
	if p.Provider != nil {
		return p.Provider.(storage.PrefixLister).ListPrefix(ctx, prefix)
	}
	return p.keys, p.err
}

func TestLocateDoesNotReadOrPromoteLegacyOriginal(t *testing.T) {
	// Bounded real bytes, not a 30-byte metadata-only fixture.
	data := strings.Repeat("x", 1<<20)
	sum := sha256.Sum256([]byte(data))
	hash := hex.EncodeToString(sum[:])
	key := path.Join("bot-a", hash[:2], hash+".txt")
	workspace := &locatorStoreProbe{Provider: localfs.New(t.TempDir())}
	spill := &locatorStoreProbe{Provider: localfs.New(t.TempDir())}
	canonical := &locatorStoreProbe{Provider: localfs.NewOriginals(t.TempDir())}
	if err := spill.Provider.Put(t.Context(), key, strings.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(nil, fallback.NewDurable(workspace, canonical, spill))
	for _, required := range []bool{false, true} {
		svc.SetRequirePersistence(required)
		for range 3 {
			asset, err := svc.Locate(t.Context(), "bot-a", hash)
			if err != nil || asset.BotID != "bot-a" || asset.ContentHash != hash || asset.Mime != "text/plain" {
				t.Fatalf("locator: %+v %v", asset, err)
			}
		}
	}
	for _, p := range []*locatorStoreProbe{workspace, spill, canonical} {
		if p.opens != 0 || p.puts != 0 || p.lists == 0 {
			t.Fatalf("lookup used payload I/O or failed to list: %+v", p)
		}
	}
	if _, err := svc.Locate(t.Context(), "bot-b", hash); !errors.Is(err, ErrAssetNotFound) {
		t.Fatalf("cross-Bot lookup=%v", err)
	}
	// Actual delivery retains the existing copy-on-read migration. It is not a
	// memory-side original copy and must never be described as side-effect-free.
	reader, _, err := svc.Open(t.Context(), "bot-a", hash)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(got) != data || canonical.puts != 1 || spill.opens == 0 {
		t.Fatalf("delivery/migration lost: %v puts=%d opens=%d", err, canonical.puts, spill.opens)
	}
}

func TestLocateDoesNotAcknowledgeContentIntegrity(t *testing.T) {
	root := t.TempDir()
	svc := NewService(nil, localfs.NewOriginals(root))
	svc.SetRequirePersistence(true)
	asset, err := svc.Ingest(t.Context(), IngestInput{BotID: "bot-a", Mime: "text/plain", Reader: strings.NewReader("synthetic original")})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "bot-a", asset.StorageKey), []byte("corrupt synthetic bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	located, err := svc.Locate(t.Context(), "bot-a", asset.ContentHash)
	if err != nil || located.ContentHash != asset.ContentHash {
		t.Fatalf("expected locator, not a content verdict: %+v %v", located, err)
	}
	reader, _, err := svc.OpenSeekable(t.Context(), "bot-a", asset.ContentHash)
	if reader != nil {
		_ = reader.Close()
	}
	if err == nil {
		t.Fatal("actual delivery bypassed integrity verification")
	}
}

func TestLocateRejectsUntrustedKeysAndNeverFallsBackToOpen(t *testing.T) {
	hash := strings.Repeat("a", 64)
	probe := &locatorStoreProbe{keys: []string{
		"other-bot/aa/" + hash + ".txt",
		"bot-a/bb/" + hash + ".txt",
		"bot-a/aa/" + hash + "0.txt",
		"bot-a/../" + hash + ".txt",
		"bot-a/aa/../../private",
	}}
	svc := NewService(nil, probe)
	if _, err := svc.Locate(t.Context(), "bot-a", hash); !errors.Is(err, ErrAssetNotFound) {
		t.Fatal(err)
	}
	if probe.opens != 0 || probe.puts != 0 || probe.lists != 1 {
		t.Fatal(probe)
	}
	for _, malformed := range []string{"../private", "https://private.invalid", strings.ToUpper(hash), "sha256:" + hash} {
		if _, err := svc.Locate(t.Context(), "bot-a", malformed); !errors.Is(err, ErrAssetNotFound) {
			t.Fatal(err)
		}
	}
	if probe.lists != 1 {
		t.Fatal("malformed hash reached storage")
	}
	cause := errors.New("synthetic private storage failure")
	probe.err = cause
	if _, err := svc.Locate(t.Context(), "bot-a", hash); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	// An ordinary Provider with no metadata capability must not be opened.
	svc = NewService(nil, struct{ storage.Provider }{})
	if _, err := svc.Locate(t.Context(), "bot-a", hash); !errors.Is(err, ErrLocatorUnsupported) {
		t.Fatal(err)
	}
}
