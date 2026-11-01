package jev

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientAskSuccessAndAnswers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("auth header %q", got)
		}
		var body struct {
			Model     string              `json:"model"`
			State     string              `json:"state"`
			Questions map[string]Question `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		if body.Model != "jev-latest" || body.State != "state-text" || len(body.Questions) != 1 {
			t.Errorf("body %+v", body)
		}
		if q := body.Questions["reply_mode"]; q.Type != "choice" || q.Instructions == "" || len(q.Criteria) == 0 {
			t.Errorf("question %+v", q)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-1.14.0","answers":{"reply_mode":{"type":"choice","choice":"fast_reply","confidence":0.87,"probabilities":{"fast_reply":0.87}}},"usage":{"input_tokens":574,"output_tokens":12}}`))
	}))
	defer server.Close()
	client := &Client{Endpoint: server.URL, APIKey: "test-key", Model: "jev-latest"}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	response, err := client.Ask(ctx, "state-text", map[string]Question{
		"reply_mode": NewChoice("criteria text", map[string]string{"fast_reply": "send", "no_fast_reply": "wait"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if choice, answer, ok := response.Choice("reply_mode"); !ok || choice != "fast_reply" || answer.Confidence == nil || *answer.Confidence != 0.87 {
		t.Fatalf("choice=%q answer=%+v ok=%v", choice, answer, ok)
	}
	if response.Usage.InputTokens != 574 {
		t.Fatalf("usage %+v", response.Usage)
	}
	if _, _, ok := response.Choice("missing"); ok {
		t.Fatal("missing answer reported present")
	}
}

// Offline triple: 5xx, slow (timeout), invalid JSON — each classified
// per design v3 §6.5 and never retried (single request per call).
func TestClientOfflineTriple(t *testing.T) {
	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer server.Close()
		client := &Client{Endpoint: server.URL, APIKey: "k", Model: "jev-latest"}
		_, err := client.Ask(context.Background(), "s", map[string]Question{"q": NewNoul("i", "y", "n")})
		assertKind(t, err, ReasonInvalidResponse)
	})
	t.Run("slow gateway", func(t *testing.T) {
		release := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}))
		defer server.Close()
		defer close(release)
		client := &Client{Endpoint: server.URL, APIKey: "k", Model: "jev-latest"}
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		_, err := client.Ask(ctx, "s", map[string]Question{"q": NewNoul("i", "y", "n")})
		assertKind(t, err, ReasonTimeout)
	})
	t.Run("invalid json", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("<html>gateway error page</html>"))
		}))
		defer server.Close()
		client := &Client{Endpoint: server.URL, APIKey: "k", Model: "jev-latest"}
		_, err := client.Ask(context.Background(), "s", map[string]Question{"q": NewNoul("i", "y", "n")})
		assertKind(t, err, ReasonInvalidResponse)
		var jevErr *Error
		if errors.As(err, &jevErr) && len(jevErr.Body) == 0 {
			t.Fatal("response body snippet missing for logs")
		}
	})
	t.Run("auth error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "denied", http.StatusUnauthorized)
		}))
		defer server.Close()
		client := &Client{Endpoint: server.URL, APIKey: "bad", Model: "jev-latest"}
		_, err := client.Ask(context.Background(), "s", map[string]Question{"q": NewNoul("i", "y", "n")})
		assertKind(t, err, ReasonAuthError)
	})
	t.Run("bad request", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"error":{"message":"a System One request names no model"}}`, http.StatusBadRequest)
		}))
		defer server.Close()
		client := &Client{Endpoint: server.URL, APIKey: "k", Model: ""}
		_, err := client.Ask(context.Background(), "s", map[string]Question{"q": NewNoul("i", "y", "n")})
		assertKind(t, err, ReasonBadRequest)
	})
	t.Run("unreachable gateway", func(t *testing.T) {
		client := &Client{Endpoint: "http://127.0.0.1:1/v1/systemone", APIKey: "k", Model: "jev-latest"}
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		_, err := client.Ask(ctx, "s", map[string]Question{"q": NewNoul("i", "y", "n")})
		assertKind(t, err, ReasonGatewayUnreachable)
	})
	t.Run("empty answers is invalid", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"model":"jev-1.14.0","answers":{}}`))
		}))
		defer server.Close()
		client := &Client{Endpoint: server.URL, APIKey: "k", Model: "jev-latest"}
		_, err := client.Ask(context.Background(), "s", map[string]Question{"q": NewNoul("i", "y", "n")})
		assertKind(t, err, ReasonInvalidResponse)
	})
}

func assertKind(t *testing.T, err error, kind string) {
	t.Helper()
	var jevErr *Error
	if !errors.As(err, &jevErr) {
		t.Fatalf("expected *jev.Error, got %T %v", err, err)
	}
	if jevErr.Kind != kind {
		t.Fatalf("kind %s, want %s", jevErr.Kind, kind)
	}
}
