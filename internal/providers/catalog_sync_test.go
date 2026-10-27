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

type lifecycleCatalogFixture struct {
	providerTestQueries
	mu        sync.Mutex
	snapshots [][]catalogsync.Model
}

func (q *lifecycleCatalogFixture) ApplyModelCatalog(_ context.Context, _ string, _ []byte, _ string, records []catalogsync.Model) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.snapshots = append(q.snapshots, append([]catalogsync.Model(nil), records...))
	return nil
}
func (q *lifecycleCatalogFixture) snapshotIDs() [][]string {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([][]string, 0, len(q.snapshots))
	for _, records := range q.snapshots {
		var ids []string
		for _, m := range records {
			ids = append(ids, m.ID)
		}
		out = append(out, ids)
	}
	return out
}

func TestCatalogSyncContinuousLifecycleRetryAndCancellation(t *testing.T) {
	var clock atomic.Int64
	clock.Store(time.Unix(1_000_000, 0).Unix())
	var mode atomic.Int64
	var gets, nonGets atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			nonGets.Add(1)
			http.Error(w, "inference not allowed", 405)
			return
		}
		gets.Add(1)
		switch mode.Load() {
		case 1:
			http.Error(w, "upstream unavailable", 503)
		case 2:
			_, _ = w.Write([]byte(`{"data":[{"id":"fixture/beta"}]}`))
		case 3:
			_, _ = w.Write([]byte(`{"data":[{"id":"fixture/gamma"}]}`))
		default:
			_, _ = w.Write([]byte(`{"data":[{"id":"fixture/alpha"}]}`))
		}
	}))
	defer server.Close()
	id := pgtype.UUID{Bytes: [16]byte{5}, Valid: true}
	q := &lifecycleCatalogFixture{providerTestQueries: providerTestQueries{provider: sqlc.Provider{ID: id, Enable: true, ClientType: string(models.ClientTypeOpenAICompletions), Config: []byte(`{"base_url":"` + server.URL + `"}`)}}}
	svc := NewService(nil, q, "")
	svc.catalogSync.now = func() time.Time { return time.Unix(clock.Load(), 0) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	completed := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.runModelCatalogSync(ctx, func(context.Context) (string, error) { return id.String(), nil }, ticks, func() { completed <- struct{}{} })
	}()
	wait := func() {
		t.Helper()
		select {
		case <-completed:
		case <-time.After(5 * time.Second):
			t.Fatal("lifecycle iteration timed out")
		}
	}
	step := func(delta time.Duration) {
		t.Helper()
		clock.Add(int64(delta / time.Second))
		ticks <- time.Unix(clock.Load(), 0)
		wait()
	}
	wait()
	if got := q.snapshotIDs(); len(got) != 1 || len(got[0]) != 1 || got[0][0] != "fixture/alpha" {
		t.Fatalf("initial snapshots=%v", got)
	}
	before := gets.Load()
	mode.Store(1)
	step(catalogSyncInterval - time.Minute)
	if gets.Load() != before {
		t.Fatal("refreshed before success interval")
	}
	step(time.Minute) // first scheduled refresh fails
	if gets.Load() != before+1 || len(q.snapshotIDs()) != 1 {
		t.Fatalf("failure mutated catalog: gets=%d snapshots=%v", gets.Load(), q.snapshotIDs())
	}
	step(catalogRetryMin / 2)
	if gets.Load() != before+1 {
		t.Fatal("retried before backoff")
	}
	step(catalogRetryMin / 2) // retry fails; next backoff doubles
	if gets.Load() != before+2 || len(q.snapshotIDs()) != 1 {
		t.Fatal("failed retry changed catalog")
	}
	mode.Store(2)
	step(catalogRetryMin)
	if gets.Load() != before+2 {
		t.Fatal("retried before doubled backoff")
	}
	step(catalogRetryMin) // recovery, second successful observation
	if got := q.snapshotIDs(); len(got) != 2 || got[1][0] != "fixture/beta" {
		t.Fatalf("recovery snapshots=%v", got)
	}
	mode.Store(3)
	step(catalogSyncInterval) // another period on same service
	if got := q.snapshotIDs(); len(got) != 3 || got[2][0] != "fixture/gamma" {
		t.Fatalf("next period snapshots=%v", got)
	}
	calls := gets.Load()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("lifecycle did not stop")
	}
	if nonGets.Load() != 0 || gets.Load() != calls {
		t.Fatalf("after stop gets=%d non-GET=%d", gets.Load(), nonGets.Load())
	}
}
