package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/catalogsync"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/models"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestCatalogSyncDedupAndBackoff(t *testing.T) {
	var s catalogSyncState
	now := time.Unix(100, 0)
	if !s.begin("one", now) || s.begin("one", now) {
		t.Fatal("in-flight operation was not deduplicated")
	}
	if !s.begin("two", now) {
		t.Fatal("other provider should be independent")
	}
	s.finish("one", now, false)
	if s.begin("one", now.Add(catalogRetryMin-time.Second)) || !s.begin("one", now.Add(catalogRetryMin)) {
		t.Fatal("first retry interval wrong")
	}
	s.finish("one", now, false)
	if s.begin("one", now.Add(2*catalogRetryMin-time.Second)) || !s.begin("one", now.Add(2*catalogRetryMin)) {
		t.Fatal("second retry interval wrong")
	}
	s.finish("one", now, true)
	if s.begin("one", now.Add(catalogSyncInterval-time.Second)) || !s.begin("one", now.Add(catalogSyncInterval)) {
		t.Fatal("success cooldown wrong")
	}
}

func TestCatalogSyncFailureNeverAppliesAndRetriesAreDeduplicated(t *testing.T) {
	var calls atomic.Int64
	var post atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet {
			post.Store(true)
		}
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	id := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	svc := NewService(nil, providerTestQueries{provider: sqlc.Provider{ID: id, Enable: true, ClientType: string(models.ClientTypeOpenAICompletions), Config: []byte(`{"base_url":"` + server.URL + `"}`)}}, "")
	var applied atomic.Int64
	apply := func(context.Context, string, []RemoteModel) error { applied.Add(1); return nil }
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = svc.SyncModelCatalog(context.Background(), id.String(), apply) }()
	}
	wg.Wait()
	if calls.Load() != 1 || applied.Load() != 0 || post.Load() {
		t.Fatalf("requests=%d applied=%d post=%t", calls.Load(), applied.Load(), post.Load())
	}
}

func TestCatalogSyncApplyErrorBacksOff(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"data":[{"id":"fixture"}]}`)) }))
	defer server.Close()
	id := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	svc := NewService(nil, providerTestQueries{provider: sqlc.Provider{ID: id, Enable: true, ClientType: string(models.ClientTypeOpenAICompletions), Config: []byte(`{"base_url":"` + server.URL + `"}`)}}, "")
	calls := 0
	apply := func(context.Context, string, []RemoteModel) error { calls++; return errors.New("storage error") }
	ran, err := svc.SyncModelCatalog(context.Background(), id.String(), apply)
	if !ran || err == nil || calls != 1 {
		t.Fatalf("ran=%v err=%v calls=%d", ran, err, calls)
	}
	ran, err = svc.SyncModelCatalog(context.Background(), id.String(), apply)
	if ran || err != nil || calls != 1 {
		t.Fatalf("cooldown ran=%v err=%v calls=%d", ran, err, calls)
	}
}

type catalogFixtureQueries struct {
	providerTestQueries
	applied []catalogsync.Model
}

func (q *catalogFixtureQueries) ApplyModelCatalog(_ context.Context, _ string, _ []byte, _ string, records []catalogsync.Model) error {
	q.applied = records
	return nil
}

func TestStoredCatalogSyncUsesOnlyDiscoveredIDsAndNoInference(t *testing.T) {
	var unexpected atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/models" {
			unexpected.Store(true)
			http.Error(w, "unexpected", 500)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"fixture-chat","context_window":4096,"max_output_tokens":512}]}`))
	}))
	defer server.Close()
	id := pgtype.UUID{Bytes: [16]byte{3}, Valid: true}
	q := &catalogFixtureQueries{providerTestQueries: providerTestQueries{provider: sqlc.Provider{ID: id, Enable: true, ClientType: string(models.ClientTypeOpenAICompletions), Config: []byte(`{"base_url":"` + server.URL + `"}`)}}}
	svc := NewService(nil, q, "")
	ran, err := svc.SyncStoredModelCatalog(context.Background(), id.String())
	if !ran || err != nil || unexpected.Load() || len(q.applied) != 1 || q.applied[0].ID != "fixture-chat" || string(q.applied[0].Config) != `{"context_window":4096,"max_output_tokens":512}` {
		t.Fatalf("ran=%v err=%v unexpected=%v applied=%+v", ran, err, unexpected.Load(), q.applied)
	}
}

type notifyingCatalogFixture struct {
	providerTestQueries
	applied chan string
}

func (q *notifyingCatalogFixture) ApplyModelCatalog(_ context.Context, id string, _ []byte, _ string, _ []catalogsync.Model) error {
	q.applied <- id
	return nil
}

func TestRunModelCatalogSyncUsesExplicitFixtureTargetAndStops(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"fixture-chat"}]}`))
	}))
	defer server.Close()
	id := pgtype.UUID{Bytes: [16]byte{4}, Valid: true}
	q := &notifyingCatalogFixture{providerTestQueries: providerTestQueries{provider: sqlc.Provider{ID: id, Enable: true, ClientType: string(models.ClientTypeOpenAICompletions), Config: []byte(`{"base_url":"` + server.URL + `"}`)}}, applied: make(chan string, 1)}
	svc := NewService(nil, q, "")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.RunModelCatalogSync(ctx, func(context.Context) (string, error) { return id.String(), nil })
	}()
	select {
	case got := <-q.applied:
		if got != id.String() {
			t.Fatalf("target = %s", got)
		}
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("initial sync did not run")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("lifecycle did not stop")
	}
}
