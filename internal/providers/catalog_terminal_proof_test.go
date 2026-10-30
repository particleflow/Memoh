package providers

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/models"
)

func TestAnthropicTerminalProofRequiredOnEveryPage(t *testing.T) {
	for _, tc := range []struct{ name, flag string }{
		{"missing", ""},
		{"null", `,"has_more":null`},
		{"string", `,"has_more":"false"`},
		{"number", `,"has_more":0`},
		{"object", `,"has_more":{}`},
		{"array", `,"has_more":[]`},
	} {
		for _, second := range []bool{false, true} {
			stage := "first_page"
			if second {
				stage = "second_page"
			}
			t.Run(tc.name+"/"+stage, func(t *testing.T) {
				s := NewService(nil, nil, "")
				calls := 0
				s.httpClient.Transport = catalogTransport(func(*http.Request) (*http.Response, error) {
					calls++
					if second && calls == 1 {
						return catalogReply(http.StatusOK, `{"data":[{"id":"first"}],"has_more":true,"last_id":"first"}`), nil
					}
					return catalogReply(http.StatusOK, `{"data":[{"id":"later"}],"last_id":"later"`+tc.flag+`}`), nil
				})
				p := sqlc.Provider{ClientType: string(models.ClientTypeAnthropicMessages), Config: []byte(`{"base_url":"https://catalog.example.test/v1","api_key":"synthetic"}`)}
				got, err := s.fetchRemoteModelsViaSDKWithOptions(t.Context(), p, false, false)
				wantCalls := 1
				if second {
					wantCalls = 2
				}
				if !errors.Is(err, ErrCatalogIncomplete) || got != nil || calls != wantCalls {
					t.Fatalf("invalid terminal accepted: models=%d calls=%d err=%v", len(got), calls, err)
				}
			})
		}
	}
}

func TestTerminalProofKeepsOtherProtocolContracts(t *testing.T) {
	for _, tc := range []struct {
		typ  models.ClientType
		body string
	}{
		{models.ClientTypeOpenAICompletions, `{"data":[{"id":"first"}]}`},
		{models.ClientTypeGoogleGenerativeAI, `{"models":[{"name":"models/first"}]}`},
		{models.ClientTypeAnthropicMessages, `{"data":[{"id":"first"}],"has_more":false}`},
	} {
		t.Run(string(tc.typ), func(t *testing.T) {
			page := map[string]json.RawMessage{}
			if err := json.Unmarshal([]byte(tc.body), &page); err != nil {
				t.Fatal(err)
			}
			_, cursor, err := catalogNextPage(tc.typ, page)
			if err != nil || cursor != "" {
				t.Fatalf("complete protocol rejected: cursor=%q err=%v", cursor, err)
			}
		})
	}
}

// Same first page; only final-page terminal proof changes. A missing required
// Anthropic has_more flag must not be guessed as false.
func TestAuditAnthropicTerminalProof(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"explicit_false", `{"data":[{"id":"later"}],"has_more":false,"last_id":"later"}`, true},
		{"missing_terminal_flag", `{"data":[{"id":"later"}],"last_id":"later"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			s := NewService(nil, nil, "")
			s.httpClient.Transport = catalogTransport(func(*http.Request) (*http.Response, error) {
				if calls.Add(1) == 1 {
					return catalogReply(200, `{"data":[{"id":"first"}],"has_more":true,"last_id":"first"}`), nil
				}
				return catalogReply(200, tc.body), nil
			})
			p := sqlc.Provider{ClientType: string(models.ClientTypeAnthropicMessages), Config: []byte(`{"base_url":"https://catalog.example.test/v1","api_key":"synthetic"}`)}
			got, err := s.fetchRemoteModelsViaSDKWithOptions(t.Context(), p, false, false)
			t.Logf("valid=%v requests=%d models=%d err=%v", tc.valid, calls.Load(), len(got), err)
			if tc.valid {
				if err != nil || len(got) != 2 {
					t.Fatalf("valid terminal rejected: %v", err)
				}
			} else if err == nil {
				t.Fatalf("missing terminal proof accepted as complete: %d models", len(got))
			}
		})
	}
}
