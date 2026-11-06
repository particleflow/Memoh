package handlers

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"path"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/storage"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

type countedRangeProvider struct {
	*localfs.Originals
	opens atomic.Int64
}

func (p *countedRangeProvider) OpenVerifiedRange(ctx context.Context, key string) (storage.VerifiedRangeReader, error) {
	p.opens.Add(1)
	return p.Originals.OpenVerifiedRange(ctx, key)
}

// A synchronous fixture cancels between scan blocks, without timing-dependent
// sleeps, a production hook, or changing the real HTTP authorization context.
type scanCancelContext struct {
	context.Context
	cancel context.CancelFunc
	checks int
}

func (c *scanCancelContext) Err() error {
	c.checks++
	if c.checks == 4 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestMediaPausedScanStillRequiresFreshHTTPAuthorization(t *testing.T) {
	f := newUploadHTTPFixture(t)
	p := &countedRangeProvider{Originals: localfs.NewOriginals(t.TempDir())}
	s := media.NewService(nil, p)
	s.SetRequirePersistence(true)
	f.handler.mediaService = s
	data := bytes.Repeat([]byte("synthetic paused range"), 180000)
	a, err := s.Ingest(t.Context(), media.IngestInput{BotID: invocationTestBotID, Mime: "application/octet-stream", Reader: bytes.NewReader(data)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	budget := &scanCancelContext{Context: ctx, cancel: cancel}
	r, err := p.OpenVerifiedRange(budget, path.Join(a.BotID, a.StorageKey))
	if r != nil || !errors.Is(err, context.Canceled) || budget.checks < 4 {
		t.Fatal("scan was not paused before a reader became available", err)
	}
	route := "/bots/" + a.BotID + "/media/" + a.ContentHash
	request := func(route, token string, want int) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.server.URL+route, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Range", "bytes=1048574-1048578")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := f.server.Client().Do(req) //nolint:gosec // G704: test-owned loopback server and fixed routes.
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		body, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != want {
			t.Fatalf("HTTP %d, want %d, err=%v", res.StatusCode, want, err)
		}
		if want == http.StatusPartialContent && !bytes.Equal(body, data[1048574:1048579]) {
			t.Fatal("resumed verified range mismatch")
		}
	}
	request(route, "", http.StatusUnauthorized)
	f.queries.revoked.Store(true)
	request(route, f.token, http.StatusForbidden)
	f.queries.revoked.Store(false)
	request(strings.Replace(route, a.BotID, "44444444-4444-4444-4444-444444444444", 1), f.token, http.StatusNotFound)
	if p.opens.Load() != 1 {
		t.Fatal("unauthorized or cross-Bot request reached paused verification")
	}
	request(route, f.token, http.StatusPartialContent)
	if p.opens.Load() != 2 {
		t.Fatal("authorized request failed to take over verification")
	}
	f.queries.revoked.Store(true)
	request(route, f.token, http.StatusForbidden)
	if p.opens.Load() != 2 {
		t.Fatal("completed cache bypassed permission revocation")
	}
}
