package mcp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/url"

	"github.com/felinics/memoh/internal/errs"
)

// Never retain a token response, URL-bearing transport error or request in
// this diagnostic. Its fields come only from fixed categories/status codes.
type oauthTokenFailure struct {
	category  string
	status    int
	oauthCode string
	cancel    error
}

func (*oauthTokenFailure) Error() string   { return "OAuth token request failed" }
func (e *oauthTokenFailure) Unwrap() error { return e.cancel }

func oauthFailure(category string, status int, oauthCode string) error {
	return errs.WrapDependency(&oauthTokenFailure{category: category, status: status, oauthCode: oauthCode}, "OAuth token unavailable")
}

// OAuthTokenFailureAttrs is the only diagnostic projection used by the
// federation logger. Unknown causes are deliberately not stringified.
func OAuthTokenFailureAttrs(err error) []slog.Attr {
	category := "token_unavailable"
	var failure *oauthTokenFailure
	if !errors.As(err, &failure) {
		return []slog.Attr{slog.String("oauth_failure_category", category)}
	}
	attrs := []slog.Attr{slog.String("oauth_failure_category", failure.category)}
	if failure.status >= 100 && failure.status <= 599 {
		attrs = append(attrs, slog.Int("oauth_http_status", failure.status))
	}
	if failure.oauthCode != "" {
		attrs = append(attrs, slog.String("oauth_error_code", failure.oauthCode))
	}
	return attrs
}

func oauthTransportFailure(err error) error {
	failure := &oauthTokenFailure{category: "transport"}
	var dns *net.DNSError
	var certificate *tls.CertificateVerificationError
	var authority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var record tls.RecordHeaderError
	var alert tls.AlertError
	var network net.Error
	var operation *net.OpError
	switch {
	case errors.Is(err, context.Canceled):
		failure.category, failure.cancel = "canceled", context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		failure.category, failure.cancel = "timeout", context.DeadlineExceeded
	case errors.As(err, &dns):
		failure.category = "dns"
	case errors.As(err, &certificate), errors.As(err, &authority), errors.As(err, &hostname), errors.As(err, &invalid), errors.As(err, &record), errors.As(err, &alert):
		failure.category = "tls"
	case errors.As(err, &network) && network.Timeout():
		failure.category = "timeout"
	case errors.As(err, &operation), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		failure.category = "connection"
	}
	return errs.WrapDependency(failure, "OAuth token unavailable")
}

func oauthResponseFailure(category string, status int, body []byte) error {
	var payload struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &payload) != nil {
		if values, err := url.ParseQuery(string(body)); err == nil {
			payload.Error = values.Get("error")
		}
	}
	code := ""
	switch payload.Error {
	case "invalid_request", "invalid_client", "invalid_grant", "unauthorized_client", "unsupported_grant_type", "invalid_scope", "access_denied", "server_error", "temporarily_unavailable":
		code, category = payload.Error, "oauth_rejected"
	}
	return oauthFailure(category, status, code)
}
