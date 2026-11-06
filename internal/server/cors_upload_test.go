package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUploadCORSPreflightAllowsOnlyExplicitHeaders(t *testing.T) {
	s := NewServer(slog.New(slog.DiscardHandler), ":0", "test-secret")
	expected := []string{"origin", "content-type", "accept", "authorization", "x-request-id", "idempotency-key", "upload-offset", "upload-length", "upload-sha256"}
	for _, requested := range []string{"authorization,content-type,upload-offset,upload-length,upload-sha256", "x-unrelated"} {
		request := httptest.NewRequest(http.MethodOptions, "/bots/bot/media/uploads/upload", nil)
		request.Header.Set("Origin", "https://upload-client.example")
		request.Header.Set("Access-Control-Request-Method", http.MethodPatch)
		request.Header.Set("Access-Control-Request-Headers", requested)
		response := httptest.NewRecorder()
		s.echo.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatal(response.Code, response.Body)
		}
		got := strings.Split(strings.ToLower(response.Header().Get("Access-Control-Allow-Headers")), ",")
		if len(got) != len(expected) {
			t.Fatal(got)
		}
		for _, want := range expected {
			found := false
			for _, header := range got {
				if strings.TrimSpace(header) == want {
					found = true
				}
			}
			if !found {
				t.Fatal("missing header", want, got)
			}
		}
		if response.Header().Get("Access-Control-Allow-Origin") != "*" || response.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Fatal("changed origin/credential policy", response.Header())
		}
	}
}
