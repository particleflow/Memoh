package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

type catalogQueries struct {
	dbstore.Queries // Any unexpected write/query panics: only this read is allowed.
	provider        sqlc.Provider
	err             error
	reads           int
}

func (q *catalogQueries) GetProviderByID(context.Context, pgtype.UUID) (sqlc.Provider, error) {
	q.reads++
	return q.provider, q.err
}

type catalogTransport func(*http.Request) (*http.Response, error)

func (f catalogTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func catalogFixture() sqlc.Provider {
	return sqlc.Provider{ID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, ClientType: "openai-completions", Enable: true, Config: []byte(`{"base_url":"https://catalog.example.test/v1","api_key":"synthetic-private-key"}`)}
}

func catalogReply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestMediaCatalogReadOnlyContract(t *testing.T) {
	q := &catalogQueries{provider: catalogFixture()}
	before := append([]byte(nil), q.provider.Config...)
	s := NewService(nil, q, "")
	calls := 0
	s.httpClient.Transport = catalogTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodGet || r.URL.String() != "https://catalog.example.test/v1/models" || r.Header.Get("Authorization") != "Bearer synthetic-private-key" || r.Header.Get("X-Magpie-Videomakers") != "1" || r.Header.Get("X-Magpie-Drawers") != "" || r.Body != http.NoBody {
			t.Fatalf("unexpected request contract")
		}
		return catalogReply(200, `{"data":[{"id":"studio/film-alpha","kind":"video","modalities":{"input":["text","image"],"output":["video"]},"api_key":"must-not-return","url":"https://private.example.test"},{"id":"studio/whisper-1"},{"id":"studio/voice-model","modalities":{"input":["audio"],"output":["text"]}}]}`), nil
	})
	result, err := s.DiscoverMediaCatalog(context.Background(), q.provider.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || q.reads != 1 || !reflect.DeepEqual(before, q.provider.Config) {
		t.Fatal("unexpected calls or mutation")
	}
	if len(result.Models) != 3 || result.Models[0].ID != "studio/film-alpha" || result.Models[0].CapabilitySource != "catalog_kind_and_modalities" || result.Models[1].CapabilitySource != "unspecified" || result.Models[2].CapabilitySource != "catalog_modalities" || result.TranscriptionStatus != "unverified" {
		t.Fatalf("unexpected catalog: %+v", result)
	}
	wire, _ := json.Marshal(result)
	for _, private := range []string{"synthetic-private-key", "catalog.example.test", "private.example.test", "must-not-return"} {
		if strings.Contains(string(wire), private) {
			t.Fatal("private data leaked")
		}
	}
}

func TestMediaCatalogFailuresAreNotEmptySuccess(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		transport bool
	}{
		{"auth", 401, "PRIVATE", false},
		{"forbidden", 403, "PRIVATE", false},
		{"not_found", 404, "PRIVATE", false},
		{"server", 500, "PRIVATE", false},
		{"redirect", 302, "", false},
		{"html", 200, "<html>PRIVATE</html>", false},
		{"missing_data", 200, "{}", false},
		{"null_data", 200, `{"data":null}`, false},
		{"wrong_shape", 200, `{"data":"PRIVATE"}`, false},
		{"trailing_json", 200, `{"data":[]}{}`, false},
		{"transport", 0, "", true},
		{"oversized", 200, strings.Repeat("x", remoteModelsResponseLimit+1), false},
		{"key_echo", 200, `{"data":[{"id":"synthetic-private-key"}]}`, false},
		{"address_echo", 200, `{"data":[{"id":"catalog.example.test/private"}]}`, false},
		{"url_id", 200, `{"data":[{"id":"https://elsewhere.test/model"}]}`, false},
		{"duplicate", 200, `{"data":[{"id":"same"},{"id":"same"}]}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := &catalogQueries{provider: catalogFixture()}
			s := NewService(nil, q, "")
			calls := 0
			s.httpClient.Transport = catalogTransport(func(*http.Request) (*http.Response, error) {
				calls++
				if tc.transport {
					return nil, errors.New("PRIVATE https://catalog.example.test synthetic-private-key")
				}
				r := catalogReply(tc.status, tc.body)
				r.Header.Set("Location", "https://other.test/models")
				return r, nil
			})
			_, err := s.DiscoverMediaCatalog(context.Background(), q.provider.ID.String())
			if err == nil || calls != 1 {
				t.Fatalf("want one failed request; calls=%d err=%v", calls, err)
			}
			for _, value := range []string{"PRIVATE", "catalog.example.test", "synthetic-private-key", "other.test"} {
				if strings.Contains(err.Error(), value) {
					t.Fatal("private error leaked")
				}
			}
		})
	}
}

func TestMediaCatalogValidEmptyAndDisabledOrForeignProvider(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		q := &catalogQueries{provider: catalogFixture()}
		s := NewService(nil, q, "")
		s.httpClient.Transport = catalogTransport(func(*http.Request) (*http.Response, error) { return catalogReply(200, `{"data":[]}`), nil })
		r, e := s.DiscoverMediaCatalog(context.Background(), q.provider.ID.String())
		if e != nil || r.Models == nil || len(r.Models) != 0 || r.TranscriptionStatus != "unverified" {
			t.Fatal(r, e)
		}
	})
	for _, mode := range []string{"disabled", "oauth", "missing", "url_userinfo", "query", "fragment", "relative", "empty", "scheme"} {
		t.Run(mode, func(t *testing.T) {
			q := &catalogQueries{provider: catalogFixture()}
			switch mode {
			case "disabled":
				q.provider.Enable = false
			case "oauth":
				q.provider.ClientType = "openai-codex"
			case "missing":
				q.err = pgx.ErrNoRows
			default:
				urls := map[string]string{"url_userinfo": "https://user@host.test/v1", "query": "https://host.test/v1?token=secret", "fragment": "https://host.test/v1#fragment", "relative": "/v1", "empty": "", "scheme": "file:///private"}
				q.provider.Config, _ = json.Marshal(map[string]string{"base_url": urls[mode]})
			}
			s := NewService(nil, q, "")
			s.httpClient.Transport = catalogTransport(func(*http.Request) (*http.Response, error) { t.Fatal("must reject before transport"); return nil, nil })
			if _, err := s.DiscoverMediaCatalog(context.Background(), q.provider.ID.String()); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}
