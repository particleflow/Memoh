package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/models"
)

// Only these two read methods exist. Any write, OAuth read/refresh, model
// import, or other query nil-panics rather than silently passing the test.
type discoveryQueries struct {
	dbstore.Queries
	provider sqlc.Provider
	missing  bool
	catalog  []sqlc.TemplateProviderTemplateModel
}

func (q discoveryQueries) GetProviderByID(_ context.Context, id pgtype.UUID) (sqlc.Provider, error) {
	if q.missing || id != q.provider.ID {
		return sqlc.Provider{}, pgx.ErrNoRows
	}
	return q.provider, nil
}

func (q discoveryQueries) ListProviderTemplateModels(context.Context, pgtype.UUID) ([]sqlc.TemplateProviderTemplateModel, error) {
	return q.catalog, nil
}

func discoveryFixture(t *testing.T, ct models.ClientType, endpoint string) discoveryQueries {
	t.Helper()
	cfg, err := json.Marshal(map[string]string{"base_url": endpoint, "api_key": "SECRET-discovery-fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	return discoveryQueries{provider: sqlc.Provider{
		ID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, Enable: true,
		ClientType: string(ct), Config: cfg,
	}}
}

func TestDiscoverModelsOnlyListsMetadata(t *testing.T) {
	for _, ct := range []models.ClientType{models.ClientTypeOpenAICompletions, models.ClientTypeOpenAIResponses, models.ClientTypeAnthropicMessages, models.ClientTypeGoogleGenerativeAI, models.ClientTypeOpenCodeGo} {
		t.Run(string(ct), func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/models") || r.ContentLength > 0 {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer SECRET-discovery-fixture-key" && r.Header.Get("X-Api-Key") != "SECRET-discovery-fixture-key" && r.Header.Get("X-Goog-Api-Key") != "SECRET-discovery-fixture-key" {
					t.Error("existing configured credential was not used")
				}
				w.Header().Set("Content-Type", "application/json")
				if ct == models.ClientTypeGoogleGenerativeAI {
					_, _ = io.WriteString(w, `{"models":[{"name":"models/image-edit-in-name","displayName":"Synthetic","supportedGenerationMethods":["embedContent"]},{"name":"models/declared-image"}],"private":"SECRET response"}`)
				} else {
					_, _ = io.WriteString(w, `{"data":[{"id":"image-edit-in-name","display_name":"Synthetic","type":"embedding","private":"SECRET response"},{"id":"declared-image"}]}`)
				}
			}))
			t.Cleanup(upstream.Close)
			q := discoveryFixture(t, ct, upstream.URL)
			q.provider.ProviderTemplateID = pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
			q.catalog = []sqlc.TemplateProviderTemplateModel{
				{ModelID: "declared-image", Config: []byte(`{"compatibilities":["image-output","image-edit","SECRET-extra-capability"],"private":"SECRET"}`)},
				{ModelID: "not-served", Config: []byte(`{"compatibilities":["image-edit"]}`)},
			}
			got, err := NewService(nil, q, "").DiscoverModels(t.Context(), q.provider.ID.String())
			if err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 1 || len(got.Models) != 2 || got.Completeness != "unknown" {
				t.Fatalf("wrong list or extra upstream call: %d %+v", calls.Load(), got)
			}
			for _, fact := range got.Models[0].Capabilities {
				if fact.Status != "unknown" || fact.Source != "unknown" {
					t.Fatal("guessed capabilities from name/type/protocol", fact)
				}
			}
			for _, fact := range got.Models[1].Capabilities {
				if fact.Name == models.CompatImageEdit || fact.Name == models.CompatImageOutput {
					if fact.Status != "declared" || fact.Source != "provider_template" {
						t.Fatal("explicit catalog evidence lost", fact)
					}
				} else if fact.Status != "unknown" {
					t.Fatal("missing declaration became a fact", fact)
				}
			}
			wantImages := ct == models.ClientTypeOpenAICompletions || ct == models.ClientTypeOpenAIResponses
			if got.Adapter.ImagesEdit != wantImages || got.Adapter.ImagesGeneration != wantImages || got.Adapter.Source != "memoh_adapter" {
				t.Fatal("adapter implementation is inaccurate", got.Adapter)
			}
			encoded, _ := json.Marshal(got)
			for _, forbidden := range []string{"SECRET", upstream.URL, "base_url", "api_key", "private", "not-served"} {
				if strings.Contains(string(encoded), forbidden) {
					t.Fatalf("non-whitelisted data leaked: %s", forbidden)
				}
			}
		})
	}
}

func TestDiscoverModelsFailsClosedWithoutSecrets(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
	}{
		{"rejected", 401, `{"error":"SECRET-token https://private.invalid/internal"}`},
		{"server_error", 500, "SECRET-token"},
		{"redirect", 302, "SECRET-token"},
		{"malformed", 200, "SECRET-token"},
		{"error_as_200", 200, `{"error":"SECRET-token"}`},
		{"null_list", 200, `{"data":null}`},
		{"wrong_list", 200, `{"data":{}}`},
		{"invalid_entry", 200, `{"data":[{}]}`},
		{"reflected_key", 200, `{"data":[{"id":"SECRET-discovery-fixture-key"}]}`},
		{"oversize", 200, `{"data":[],"padding":"` + strings.Repeat("x", remoteModelsResponseLimit) + `"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var redirectCalls atomic.Int32
			destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirectCalls.Add(1) }))
			t.Cleanup(destination.Close)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", destination.URL)
				w.WriteHeader(tc.code)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(upstream.Close)
			var logs strings.Builder
			q := discoveryFixture(t, models.ClientTypeOpenAICompletions, upstream.URL)
			_, err := NewService(slog.New(slog.NewTextHandler(&logs, nil)), q, "").DiscoverModels(t.Context(), q.provider.ID.String())
			if !errors.Is(err, ErrDiscoveryUpstream) || redirectCalls.Load() != 0 {
				t.Fatalf("expected fixed failure and no redirect: %v, %d", err, redirectCalls.Load())
			}
			if strings.Contains(err.Error()+logs.String(), "SECRET") || strings.Contains(logs.String(), upstream.URL) {
				t.Fatal("upstream diagnostic leaked")
			}
		})
	}
}

func TestDiscoverModelsUnavailableAndOAuthNeverContactUpstream(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	t.Cleanup(upstream.Close)
	for _, ct := range []models.ClientType{models.ClientTypeOpenAICodex, models.ClientTypeGitHubCopilot, models.ClientTypeEdgeSpeech} {
		q := discoveryFixture(t, ct, upstream.URL)
		_, err := NewService(nil, q, "").DiscoverModels(t.Context(), q.provider.ID.String())
		if !errors.Is(err, ErrDiscoveryUnsupported) {
			t.Fatal(err)
		}
	}
	q := discoveryFixture(t, models.ClientTypeOpenAICompletions, upstream.URL)
	q.missing = true
	_, err := NewService(nil, q, "").DiscoverModels(t.Context(), q.provider.ID.String())
	if !errors.Is(err, ErrDiscoveryUnavailable) {
		t.Fatal(err)
	}
	q.missing, q.provider.Enable = false, false
	_, err = NewService(nil, q, "").DiscoverModels(t.Context(), q.provider.ID.String())
	if !errors.Is(err, ErrDiscoveryUnavailable) || calls.Load() != 0 {
		t.Fatal("unavailable provider contacted", err, calls.Load())
	}
}
