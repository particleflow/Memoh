package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/catalogsync"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/models"
)

func TestCatalogSDKReadsDocumentedPages(t *testing.T) {
	for _, typ := range []models.ClientType{models.ClientTypeAnthropicMessages, models.ClientTypeGoogleGenerativeAI} {
		t.Run(string(typ), func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != "/tenant/v1/models" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					http.Error(w, "unexpected", http.StatusBadRequest)
					return
				}
				var body string
				if typ == models.ClientTypeAnthropicMessages {
					switch r.URL.Query().Get("after_id") {
					case "":
						body = `{"data":[{"id":"first"}],"has_more":true,"last_id":"first"}`
					case "first":
						body = `{"data":[],"has_more":true,"last_id":"empty-page"}`
					case "empty-page":
						body = `{"data":[{"id":"later"}],"has_more":false,"last_id":"later"}`
					default:
						t.Error("unexpected cursor")
					}
				} else {
					switch r.URL.Query().Get("pageToken") {
					case "":
						body = `{"models":[{"name":"models/first"}],"nextPageToken":"page-two"}`
					case "page-two":
						body = `{"models":[],"nextPageToken":"page-three"}`
					case "page-three":
						body = `{"models":[{"name":"models/later"}]}`
					default:
						t.Error("unexpected cursor")
					}
				}
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			cfg, _ := json.Marshal(map[string]string{"base_url": server.URL + "/tenant/v1", "api_key": "synthetic"})
			p := sqlc.Provider{ClientType: string(typ), Config: cfg}
			s := NewService(nil, providerTestQueries{}, "")
			got, err := s.fetchRemoteModelsViaSDKWithOptions(t.Context(), p, false, false)
			var ids []string
			for _, m := range got {
				ids = append(ids, m.ID)
			}
			if err != nil || !reflect.DeepEqual(ids, []string{"first", "later"}) || calls.Load() != 3 {
				t.Fatalf("ids=%v requests=%d err=%v", ids, calls.Load(), err)
			}
		})
	}
}

func TestCatalogSyncIncompleteDoesNotReplaceStoredSnapshot(t *testing.T) {
	for _, marker := range []string{`"has_more":true,"last_id":"first"`, `"nextPageToken":"next"`, `"next_cursor":"next"`} {
		t.Run(marker, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodGet {
					t.Error("inference is forbidden")
				}
				_, _ = fmt.Fprintf(w, `{"data":[{"id":"first"}],%s}`, marker)
			}))
			defer server.Close()
			id := pgtype.UUID{Bytes: [16]byte{44}, Valid: true}
			q := &catalogFixtureQueries{providerTestQueries: providerTestQueries{provider: sqlc.Provider{ID: id, Enable: true, ClientType: string(models.ClientTypeOpenAICompletions), Config: []byte(`{"base_url":"` + server.URL + `"}`)}}}
			old := []catalogsync.Model{{ID: "already-stored-later-page", Name: "unchanged", Config: []byte(`{"catalog_available":true}`)}}
			q.applied = old
			s := NewService(nil, q, "")
			ran, err := s.SyncStoredModelCatalog(context.Background(), id.String())
			if !ran || !errors.Is(err, ErrCatalogIncomplete) || !reflect.DeepEqual(q.applied, old) || calls.Load() != 1 {
				t.Fatalf("ran=%v err=%v applied=%+v requests=%d", ran, err, q.applied, calls.Load())
			}
		})
	}
}

func TestCatalogPageFailuresNeverReturnAPrefix(t *testing.T) {
	for _, tc := range []struct {
		name, second string
		status       int
	}{
		{"second_page_http", `PRIVATE`, http.StatusForbidden},
		{"second_page_invalid", `{"data":null}`, http.StatusOK},
		{"cursor_loop", `{"data":[],"has_more":true,"last_id":"first"}`, http.StatusOK},
		{"missing_cursor", `{"data":[],"has_more":true}`, http.StatusOK},
		{"duplicate_id", `{"data":[{"id":"first"}],"has_more":false}`, http.StatusOK},
		{"invalid_flag", `{"data":[],"has_more":"false"}`, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			s := NewService(nil, nil, "")
			s.httpClient.Transport = catalogTransport(func(r *http.Request) (*http.Response, error) {
				if calls.Add(1) == 1 {
					return catalogReply(http.StatusOK, `{"data":[{"id":"first"}],"has_more":true,"last_id":"first"}`), nil
				}
				if r.URL.Query().Get("after_id") != "first" || r.URL.Host != "catalog.example.test" || r.URL.Path != "/v1/models" || r.Header.Get("x-api-key") != "synthetic" {
					t.Error("page changed destination, credential or cursor contract")
				}
				return catalogReply(tc.status, tc.second), nil
			})
			p := sqlc.Provider{ClientType: string(models.ClientTypeAnthropicMessages), Config: []byte(`{"base_url":"https://catalog.example.test/v1","api_key":"synthetic"}`)}
			got, err := s.fetchRemoteModelsViaSDKWithOptions(t.Context(), p, false, false)
			if err == nil || len(got) != 0 || calls.Load() != 2 {
				t.Fatalf("partial catalog escaped: models=%d requests=%d err=%v", len(got), calls.Load(), err)
			}
			if errors.Is(err, ErrCatalogIncomplete) && strings.Contains(err.Error(), "catalog.example.test") {
				t.Fatal("pagination validation exposed the provider address")
			}
		})
	}
}

func TestCatalogCancelBetweenPages(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s := NewService(nil, nil, "")
	calls := 0
	s.httpClient.Transport = catalogTransport(func(*http.Request) (*http.Response, error) {
		calls++
		cancel()
		return catalogReply(http.StatusOK, `{"models":[{"name":"models/first"}],"nextPageToken":"still-more"}`), nil
	})
	p := sqlc.Provider{ClientType: string(models.ClientTypeGoogleGenerativeAI), Config: []byte(`{"base_url":"https://catalog.example.test/v1"}`)}
	got, err := s.fetchRemoteModelsViaSDKWithOptions(ctx, p, false, false)
	if !errors.Is(err, context.Canceled) || len(got) != 0 || calls != 1 {
		t.Fatalf("cancelled catalog=%+v calls=%d err=%v", got, calls, err)
	}
}

func TestCatalogPaginationUsesSharedDeadlineAndByteBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	calls := 0
	rt := &catalogPaginationTransport{typ: models.ClientTypeGoogleGenerativeAI, base: catalogTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if got, _ := r.Context().Deadline(); !got.Equal(deadline) {
			t.Error("page reset the overall deadline")
		}
		// Each page fits the existing limit; their aggregate does not.
		return catalogReply(http.StatusOK, `{"models":[],"nextPageToken":"`+strconv.Itoa(calls)+`","unused":"`+strings.Repeat("x", remoteModelsResponseLimit/2)+`"}`), nil
	})}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://catalog.example.test/v1/models", http.NoBody)
	resp, err := rt.RoundTrip(req)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if resp != nil || !errors.Is(err, ErrMediaCatalogResponse) || calls != 2 {
		t.Fatalf("unbounded catalog: response=%v calls=%d err=%v", resp != nil, calls, err)
	}
}

func TestCatalogSyncRejectsChangedSnapshotAndKeepsCooldown(t *testing.T) {
	var calls atomic.Int64
	var includeLater atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet {
			t.Error("unexpected business request")
		}
		if r.Header.Get("X-Magpie-Videomakers") == "1" || includeLater.Load() {
			_, _ = w.Write([]byte(`{"data":[{"id":"first"},{"id":"later"}]}`))
		} else {
			_, _ = w.Write([]byte(`{"data":[{"id":"first"}]}`))
		}
	}))
	defer server.Close()
	id := pgtype.UUID{Bytes: [16]byte{45}, Valid: true}
	q := &catalogFixtureQueries{providerTestQueries: providerTestQueries{provider: sqlc.Provider{ID: id, Enable: true, ClientType: string(models.ClientTypeOpenAICompletions), Config: []byte(`{"base_url":"` + server.URL + `"}`)}}}
	s := NewService(nil, q, "")
	now := time.Unix(100, 0)
	s.catalogSync.now = func() time.Time { return now }
	if ran, err := s.SyncStoredModelCatalog(t.Context(), id.String()); !ran || !errors.Is(err, ErrCatalogIncomplete) || q.applied != nil {
		t.Fatalf("changing snapshot applied: ran=%v err=%v records=%+v", ran, err, q.applied)
	}
	before := calls.Load()
	if ran, err := s.SyncStoredModelCatalog(t.Context(), id.String()); ran || err != nil || calls.Load() != before {
		t.Fatal("failed refresh bypassed existing cooldown")
	}
	includeLater.Store(true)
	now = now.Add(catalogRetryMin)
	if ran, err := s.SyncStoredModelCatalog(t.Context(), id.String()); !ran || err != nil || len(q.applied) != 2 {
		t.Fatalf("complete retry failed: ran=%v err=%v records=%+v", ran, err, q.applied)
	}
}

func TestCatalogPagesHaveNoFixedPageCountOrCursorDestination(t *testing.T) {
	const pages = 131
	const opaque = "https://different.example.test/path?value=one&value=two"
	calls := 0
	s := NewService(nil, nil, "")
	s.httpClient.Transport = catalogTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "catalog.example.test" || r.URL.Path != "/tenant/v1/models" || r.Header.Get("x-goog-api-key") != "synthetic" {
			t.Error("pagination changed origin, path or credentials")
		}
		if calls == 2 && r.URL.Query().Get("pageToken") != opaque {
			t.Error("opaque cursor was not preserved")
		}
		next := strconv.Itoa(calls)
		switch calls {
		case 1:
			next = opaque
		case pages:
			next = ""
		}
		body, _ := json.Marshal(map[string]any{"models": []map[string]string{{"name": "models/" + strconv.Itoa(calls)}}, "nextPageToken": next})
		return catalogReply(http.StatusOK, string(body)), nil
	})
	p := sqlc.Provider{ClientType: string(models.ClientTypeGoogleGenerativeAI), Config: []byte(`{"base_url":"https://catalog.example.test/tenant/v1","api_key":"synthetic"}`)}
	got, err := s.fetchRemoteModelsViaSDKWithOptions(t.Context(), p, false, false)
	if err != nil || len(got) != pages || calls != pages {
		t.Fatalf("complete pages: models=%d calls=%d err=%v", len(got), calls, err)
	}
}

func TestCatalogConcurrentRefreshDoesNotReplaceSnapshotAfterCancel(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	id := pgtype.UUID{Bytes: [16]byte{46}, Valid: true}
	q := &catalogFixtureQueries{providerTestQueries: providerTestQueries{provider: sqlc.Provider{ID: id, Enable: true, ClientType: string(models.ClientTypeOpenAICompletions), Config: []byte(`{"base_url":"https://catalog.example.test/v1"}`)}}}
	old := []catalogsync.Model{{ID: "retained", Config: []byte(`{"catalog_available":true}`)}}
	q.applied = old
	s := NewService(nil, q, "")
	var calls atomic.Int64
	s.httpClient.Transport = catalogTransport(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		close(entered)
		<-release
		return catalogReply(http.StatusOK, `{"data":[]}`), nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.SyncStoredModelCatalog(ctx, id.String()); done <- err }()
	<-entered
	if ran, err := s.SyncStoredModelCatalog(t.Context(), id.String()); ran || err != nil {
		t.Errorf("concurrent refresh was not deduplicated: %v %v", ran, err)
	}
	cancel()
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) || !reflect.DeepEqual(q.applied, old) || calls.Load() != 1 {
		t.Fatalf("cancel replaced snapshot: err=%v calls=%d records=%+v", err, calls.Load(), q.applied)
	}
}
