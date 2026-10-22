package tools

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// imagesServer answers the images endpoint with status first and with one
// base64 image afterwards, counting the generation requests.
func imagesServer(t *testing.T, first int, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			w.WriteHeader(first)
			_, _ = w.Write([]byte(`{"error":{"message":"try later"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(testPNGBytes) + `"}]}`))
	}))
	t.Cleanup(server.Close)
	return server
}

// The persistent image service owns the only attempt, including rate limits.
func TestGenerateOpenAIImagesImageDoesNotRetryRateLimit(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := imagesServer(t, http.StatusTooManyRequests, &calls)
	_, err := generateOpenAIImagesImage(context.Background(), nil, http.DefaultClient, server.URL, "openai-key", "gpt-image-1", "a cube", "1024x1024")
	if err == nil || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d, want one failed attempt", err, calls.Load())
	}
}

// A server error may follow an image the provider already made and billed, so
// it is not made again.
func TestGenerateOpenAIImagesImageDoesNotRetryServerError(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := imagesServer(t, http.StatusInternalServerError, &calls)
	if _, err := generateOpenAIImagesImage(context.Background(), nil, http.DefaultClient, server.URL, "openai-key", "gpt-image-1", "a cube", "1024x1024"); err == nil {
		t.Fatal("generateOpenAIImagesImage() error = nil, want the server error")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("image requests = %d, want 1", got)
	}
}
