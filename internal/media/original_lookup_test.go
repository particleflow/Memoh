package media

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/storage"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

type originalLookupProbe struct {
	storage.Provider
	calls int
	err   error
}

func (p *originalLookupProbe) ListPrefix(context.Context, string) ([]string, error) {
	p.calls++
	return nil, p.err
}

func TestOriginalLookupRejectsMalformedHashBeforeStorage(t *testing.T) {
	for _, hash := range []string{
		"ab", "not-a-hash", strings.Repeat("a", 63), strings.Repeat("a", 65),
		strings.Repeat("g", 64), strings.Repeat("A", 64),
		" " + strings.Repeat("a", 64), "../private",
		"%2e%2e%2fprivate", "%252e%252e%252fprivate",
		"sha256:" + strings.Repeat("a", 64), "sha256%3A..%2F..%2Fconfig.toml",
	} {
		t.Run(hash, func(t *testing.T) {
			provider := &originalLookupProbe{err: errors.New("storage must not be reached")}
			service := NewService(nil, provider)
			service.SetRequirePersistence(true)
			reader, _, err := service.Open(context.Background(), "bot-a", hash)
			if !errors.Is(err, ErrAssetNotFound) || reader != nil {
				t.Fatalf("Open() = %v, %v; want no reader and ErrAssetNotFound", reader, err)
			}
			if provider.calls != 0 {
				t.Fatal("malformed hash reached storage")
			}
		})
	}
}

func TestOriginalLookupPreservesStorageFailures(t *testing.T) {
	cause := errors.New("synthetic storage failure")
	provider := &originalLookupProbe{err: cause}
	service := NewService(nil, provider)
	service.SetRequirePersistence(true)
	_, _, err := service.Open(context.Background(), "bot-a", strings.Repeat("a", 64))
	if !errors.Is(err, cause) || errors.Is(err, ErrAssetNotFound) || provider.calls != 1 {
		t.Fatalf("storage failure reclassified or skipped: %v, calls=%d", err, provider.calls)
	}
}

func TestOriginalLookupMatchesLegacyMalformedHashClassification(t *testing.T) {
	for _, durable := range []bool{false, true} {
		var provider storage.Provider = localfs.New(t.TempDir())
		if durable {
			provider = localfs.NewOriginals(t.TempDir())
		}
		service := NewService(nil, provider)
		service.SetRequirePersistence(durable)
		_, _, err := service.Open(context.Background(), "bot-a", "not-a-hash")
		if !errors.Is(err, ErrAssetNotFound) {
			t.Fatalf("durable=%v: expected ErrAssetNotFound, got %v", durable, err)
		}
	}
}
