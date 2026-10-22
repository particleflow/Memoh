package mcp

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type oauthDiagnosticTransport func(*http.Request) (*http.Response, error)

func (f oauthDiagnosticTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type oauthDiagnosticReadFailure struct{}

func (oauthDiagnosticReadFailure) Read([]byte) (int, error) {
	return 0, errors.New("SECRET-response-body")
}
func (oauthDiagnosticReadFailure) Close() error { return nil }

func assertOAuthDiagnostic(t *testing.T, err error, category string, status int, code string) {
	t.Helper()
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("missing or unsafe diagnostic: %v", err)
	}
	var output bytes.Buffer
	slog.New(slog.NewJSONHandler(&output, nil)).LogAttrs(t.Context(), slog.LevelWarn, "refresh failed", OAuthTokenFailureAttrs(fmt.Errorf("outer: %w", err))...)
	if strings.Contains(output.String(), "SECRET") || strings.Contains(output.String(), "https://") {
		t.Fatal("private token response or endpoint reached the log")
	}
	var entry map[string]any
	if json.Unmarshal(output.Bytes(), &entry) != nil || entry["oauth_failure_category"] != category {
		t.Fatalf("wrong category: %s", &output)
	}
	if status == 0 {
		if _, exists := entry["oauth_http_status"]; exists {
			t.Fatal("invented HTTP response status")
		}
	} else if entry["oauth_http_status"] != float64(status) {
		t.Fatal("missing HTTP status")
	}
	if code == "" {
		if _, exists := entry["oauth_error_code"]; exists {
			t.Fatal("unapproved provider error code")
		}
	} else if entry["oauth_error_code"] != code {
		t.Fatal("missing allowlisted OAuth error code")
	}
	for key := range entry {
		switch key {
		case "time", "level", "msg", "oauth_failure_category", "oauth_http_status", "oauth_error_code":
		default:
			t.Fatalf("unapproved diagnostic field %q", key)
		}
	}
}

func TestOAuthRefreshResponseDiagnosticsAreBoundedAndSecretFree(t *testing.T) {
	for _, tc := range []struct {
		name, body, category, code string
		status                     int
		readFailure                bool
	}{
		{name: "invalid_grant", status: 400, body: `{"error":"invalid_grant","error_description":"SECRET-token-url-header"}`, category: "oauth_rejected", code: "invalid_grant"},
		{name: "invalid_client", status: 401, body: `{"error":"invalid_client","error_description":"SECRET-client"}`, category: "oauth_rejected", code: "invalid_client"},
		{name: "form_error", status: 400, body: `error=invalid_scope&error_description=SECRET-private-scope`, category: "oauth_rejected", code: "invalid_scope"},
		{name: "unknown_error", status: 400, body: `{"error":"SECRET-code","error_description":"SECRET-body"}`, category: "http_error"},
		{name: "upstream", status: 503, body: `SECRET-proxy-body`, category: "http_error"},
		{name: "rate_limited", status: 429, body: `SECRET-body`, category: "http_error"},
		{name: "oauth_error_with_200", status: 200, body: `{"error":"invalid_grant","error_description":"SECRET"}`, category: "oauth_rejected", code: "invalid_grant"},
		{name: "malformed", status: 200, body: `{"SECRET":`, category: "response_invalid"},
		{name: "missing_token", status: 200, body: `{"refresh_token":"SECRET"}`, category: "response_invalid"},
		{name: "response_read", status: 200, category: "response_read", readFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			svc := NewOAuthService(slog.New(slog.DiscardHandler), nil, "")
			svc.httpClient = &http.Client{Transport: oauthDiagnosticTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				if r.Method != http.MethodPost || r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "SECRET-refresh" {
					t.Fatal("refresh request contract changed")
				}
				body := io.NopCloser(strings.NewReader(tc.body))
				if tc.readFailure {
					body = oauthDiagnosticReadFailure{}
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"X-SECRET": []string{"SECRET-header"}}, Body: body, Request: r}, nil
			})}
			token, err := svc.refreshToken(t.Context(), "https://SECRET.invalid/SECRET?key=SECRET", "SECRET-refresh", "SECRET-client", "SECRET-resource")
			if token != nil || calls != 1 {
				t.Fatal("failure returned token or automatically retried")
			}
			assertOAuthDiagnostic(t, err, tc.category, tc.status, tc.code)
		})
	}
}

func TestOAuthRefreshTransportDiagnosticsDoNotRetainURLs(t *testing.T) {
	for _, tc := range []struct {
		name, category string
		cause          error
	}{
		{"canceled", "canceled", context.Canceled},
		{"timeout", "timeout", context.DeadlineExceeded},
		{"dns", "dns", &net.DNSError{Name: "SECRET.invalid", Server: "SECRET-server", Err: "SECRET"}},
		{"connection", "connection", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("SECRET-address")}},
		{"tls_certificate", "tls", &tls.CertificateVerificationError{Err: errors.New("SECRET-certificate")}},
		{"tls_hostname", "tls", x509.HostnameError{Host: "SECRET.invalid"}},
		{"tls_record", "tls", tls.RecordHeaderError{Msg: "SECRET-record"}},
		{"tls_alert", "tls", tls.AlertError(42)},
		{"eof", "connection", io.EOF},
		{"other", "transport", errors.New("SECRET-transport")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			svc := NewOAuthService(slog.New(slog.DiscardHandler), nil, "")
			svc.httpClient = &http.Client{Transport: oauthDiagnosticTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, &url.Error{Op: "Post", URL: "https://SECRET.invalid/SECRET", Err: tc.cause}
			})}
			_, err := svc.refreshToken(t.Context(), "https://SECRET.invalid/SECRET", "SECRET-refresh", "SECRET-client", "")
			assertOAuthDiagnostic(t, err, tc.category, 0, "")
			if calls != 1 {
				t.Fatal("transport failure automatically retried")
			}
			for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
				if errors.Is(tc.cause, sentinel) != errors.Is(err, sentinel) {
					t.Fatal("cancellation identity changed")
				}
			}
		})
	}
	_, err := NewOAuthService(slog.New(slog.DiscardHandler), nil, "").refreshToken(t.Context(), ":SECRET-invalid-endpoint", "SECRET", "SECRET", "")
	assertOAuthDiagnostic(t, err, "request_invalid", 0, "")
}

func TestOAuthUnknownFailureLogProjectionDoesNotStringifyCause(t *testing.T) {
	var output bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&output, nil))
	log.LogAttrs(t.Context(), slog.LevelWarn, "token unavailable", OAuthTokenFailureAttrs(errors.New("SECRET https://SECRET.invalid Authorization: SECRET"))...)
	if strings.Contains(output.String(), "SECRET") || !strings.Contains(output.String(), `"oauth_failure_category":"token_unavailable"`) {
		t.Fatal("unknown cause escaped safe projection")
	}
}
