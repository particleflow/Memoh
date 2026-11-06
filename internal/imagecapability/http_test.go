package imagecapability

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type observedBody struct {
	io.Reader
	reads  int
	closed bool
}

func (b *observedBody) Read(p []byte) (int, error) { b.reads++; return b.Reader.Read(p) }
func (b *observedBody) Close() error               { b.closed = true; return nil }

func TestSDKHTTPBoundary(t *testing.T) {
	for _, mode := range []string{"known_oversize", "chunked_oversize", "non_200"} {
		t.Run(mode, func(t *testing.T) {
			var calls int
			body := &observedBody{Reader: strings.NewReader("private synthetic response")}
			p, _ := fixture(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected HTTP route") })
			p.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.GetBody != nil {
					t.Error("SDK request has a replay body")
				}
				response := &http.Response{StatusCode: http.StatusOK, Body: body, ContentLength: -1, Header: make(http.Header), Request: r}
				switch mode {
				case "known_oversize":
					response.ContentLength = maxResponseBytes + 1
				case "chunked_oversize":
					body.Reader = io.MultiReader(io.LimitReader(zeroReader{}, maxResponseBytes), strings.NewReader("x"))
				case "non_200":
					response.StatusCode = http.StatusBadGateway
				}
				return response, nil
			})
			result := call(t, p, tools.SessionContext{BotID: "synthetic-bot"}, "generate_image", `{"prompt":"synthetic"}`)
			if calls != 1 || result.IsError || !strings.Contains(result.Result.String(), `"state":"unknown"`) || strings.Contains(result.Result.String(), "private") || !body.closed {
				t.Fatalf("calls=%d closed=%v result=%s", calls, body.closed, result.Result.String())
			}
			if mode != "chunked_oversize" && body.reads != 0 {
				t.Fatal("unconfirmed response body was read")
			}
		})
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestResponseReadLimit(t *testing.T) {
	for _, tc := range []struct {
		input   string
		limited bool
	}{{"1234", false}, {"12345", true}} {
		body := &imageResponseBody{ReadCloser: io.NopCloser(strings.NewReader(tc.input)), remaining: 4}
		got, err := io.ReadAll(body)
		if string(got) != "1234" || errors.Is(err, errResponseLimit) != tc.limited || (!tc.limited && err != nil) {
			t.Fatalf("data=%q err=%v", got, err)
		}
	}
}

func TestSDKPreservesHTTPTimeout(t *testing.T) {
	var calls atomic.Int32
	p, _ := fixture(t, func(_ http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
			t.Error("client timeout did not cancel HTTP")
		}
	})
	p.http.Timeout = 50 * time.Millisecond
	result := call(t, p, tools.SessionContext{BotID: "synthetic-bot"}, "generate_image", `{"prompt":"synthetic"}`)
	if calls.Load() != 1 || result.IsError || !strings.Contains(result.Result.String(), `"state":"unknown"`) {
		t.Fatalf("calls=%d result=%s", calls.Load(), result.Result.String())
	}
}

func TestLegacyPoliciesNeverDispatch(t *testing.T) {
	for _, raw := range []string{
		`{"protocol":"openai-images","model_mode":"omit"}`,
		`{"protocol":"openai-images","model_mode":"configured","edits":true}`,
	} {
		p, _ := fixture(t, func(http.ResponseWriter, *http.Request) { t.Error("legacy policy dispatched") })
		p.resolve = func(context.Context, string) (selection, bool, error) {
			cfg, enabled, err := parseConfig(sqlc.Provider{ClientType: "openai-completions", Config: []byte(`{"base_url":"https://provider.example.invalid/v1","images_api":` + raw + `}`)})
			return selection{config: cfg}, enabled, err
		}
		if _, err := p.Tools(t.Context(), tools.SessionContext{BotID: "synthetic-bot"}); err == nil {
			t.Fatal("legacy policy was silently changed")
		}
	}
}
