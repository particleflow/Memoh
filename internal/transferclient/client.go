// Package transferclient adapts the frozen PR #8 media API for an existing
// delivery host. It owns neither credentials, filesystem locations nor workers.
package transferclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	MaxChunkBytes = 8 << 20
	copyBytes     = 256 << 10
	maxJSONBytes  = 64 << 10
)

var (
	ErrProtocol      = errors.New("invalid transfer response")
	ErrTransport     = errors.New("transfer connection failed")
	ErrAuthorization = errors.New("transfer authorization unavailable")
	ErrIntegrity     = errors.New("transfer integrity check failed")
	ErrInvalid       = errors.New("invalid transfer input")
)

// Authorization is supplied by the approved host for every request. It must
// resolve the configured device credential; this package cannot mint or widen it.
type Authorization func(context.Context) (string, error)

type Client struct {
	base      string
	http      http.Client
	auth      Authorization
	selection string
}

// New accepts HTTPS only. The supplied transport must validate the server's
// certificate and hostname. Test roots belong only in an isolated TLS fixture.
// Redirects and cookie jars are disabled, and requests have a finite deadline.
func New(base string, client *http.Client, auth Authorization) (*Client, error) {
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || auth == nil {
		return nil, ErrInvalid
	}
	h := http.Client{Timeout: 150 * time.Second}
	transport := http.DefaultTransport
	if client != nil {
		if client.Transport != nil {
			transport = client.Transport
		}
		if client.Timeout > 0 && client.Timeout < h.Timeout {
			h.Timeout = client.Timeout
		}
	}
	t, ok := transport.(*http.Transport)
	if !ok || t.DialTLS != nil || t.DialTLSContext != nil || t.TLSClientConfig != nil && t.TLSClientConfig.InsecureSkipVerify { //nolint:staticcheck // Reject the deprecated hook too: it can bypass certificate verification.
		return nil, ErrInvalid
	}
	t = t.Clone()
	h.Transport = t
	if t.TLSClientConfig == nil {
		t.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	} else {
		t.TLSClientConfig.MinVersion = max(t.TLSClientConfig.MinVersion, tls.VersionTLS12)
	}
	t.TLSClientConfig.ServerName = u.Hostname()
	t.DisableCompression = true
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{base: strings.TrimRight(base, "/"), http: h, auth: auth}, nil
}

// NewTask uses the explicit task-authenticated adapter namespace. The original
// file protocol is unchanged, and credentials still come from the approved host.
func NewTask(base, operation, delivery string, client *http.Client, auth Authorization) (*Client, error) {
	if !validID(operation) || !validID(delivery) {
		return nil, ErrInvalid
	}
	return New(strings.TrimRight(base, "/")+"/internal/computer-transfers/"+operation+"/"+delivery, client, auth)
}

func (c *Client) CloseIdleConnections() { c.http.CloseIdleConnections() }

type Spec struct {
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
	Mime      string `json:"mime"`
}

func (s Spec) valid() bool {
	if len(s.Mime) > 255 {
		return false
	}
	m, params, err := mime.ParseMediaType(s.Mime)
	return s.SizeBytes > 0 && validHash(s.SHA256) && err == nil && len(params) == 0 && m == s.Mime
}

type Limits struct {
	MaxUploadBytes   int64 `json:"max_upload_bytes"`
	ResumableEnabled bool  `json:"resumable_enabled"`
	ChunkMaxBytes    int64 `json:"chunk_max_bytes"`
}

// Receipt mirrors PR #8's wire shape, without importing the server or runtime.
type Receipt struct {
	ID          string    `json:"id"`
	State       string    `json:"state"`
	Offset      int64     `json:"offset"`
	SizeBytes   int64     `json:"size_bytes"`
	ExpiresAt   time.Time `json:"expires_at"`
	ContentHash string    `json:"content_hash,omitempty"`
	Mime        string    `json:"mime"`
}

type HTTPError struct {
	Status     int
	RetryAfter time.Duration
}

// Memoh's error catalog and HTTP boundary use 499 for caller cancellation.
// It never proves that a mutating request did not reach its commit point.
const statusRequestCanceled = 499

func (e *HTTPError) Error() string { return fmt.Sprintf("transfer HTTP %d", e.Status) }

func validID(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

func validHash(s string) bool {
	if len(s) != 64 || s != strings.ToLower(s) {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func mediaPath(bot string) (string, error) {
	if !validID(bot) {
		return "", ErrInvalid
	}
	return "/bots/" + bot + "/media", nil
}

func (c *Client) request(ctx context.Context, method, path string, body []byte, headers http.Header) (*http.Response, error) {
	auth, err := c.auth(ctx)
	if err != nil || auth == "" || strings.ContainsAny(auth, "\r\n") {
		return nil, ErrAuthorization
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, ErrInvalid
	}
	for k, values := range headers {
		req.Header[k] = append([]string(nil), values...)
	}
	req.Header.Set("Authorization", auth)
	if c.selection != "" {
		req.Header.Set("Memoh-Transfer-Context", c.selection)
	}
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := c.http.Do(req) //nolint:gosec // Origin is approved host configuration; only validated logical IDs enter paths, and redirects are disabled.
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Never return a URL, token or transport diagnostic to a model/log.
		return nil, ErrTransport
	}
	return resp, nil
}

func statusError(resp *http.Response) error {
	retry := time.Duration(0)
	if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && seconds > 0 {
		retry = time.Duration(min(seconds, 300)) * time.Second
	} else if date, err := http.ParseTime(resp.Header.Get("Retry-After")); err == nil {
		retry = time.Until(date)
	}
	if retry < 0 {
		retry = 0
	}
	if retry > 5*time.Minute {
		retry = 5 * time.Minute
	}
	return &HTTPError{Status: resp.StatusCode, RetryAfter: retry}
}

func (c *Client) json(ctx context.Context, method, path string, body []byte, headers http.Header, want int, out any) error {
	resp, err := c.request(ctx, method, path, body, headers)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != want {
		return statusError(resp)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONBytes+1))
	if err != nil {
		return ErrTransport
	}
	if len(raw) > maxJSONBytes || json.Unmarshal(raw, out) != nil {
		return ErrProtocol
	}
	return nil
}

func (c *Client) Limits(ctx context.Context, bot string) (Limits, error) {
	var result Limits
	path, err := mediaPath(bot)
	if err == nil {
		err = c.json(ctx, http.MethodGet, path+"/limits", nil, nil, 200, &result)
	}
	if err == nil && (result.MaxUploadBytes <= 0 || result.ResumableEnabled && (result.ChunkMaxBytes <= 0 || result.ChunkMaxBytes > MaxChunkBytes)) {
		err = ErrProtocol
	}
	return result, err
}

func (c *Client) Create(ctx context.Context, bot string, spec Spec) (Receipt, error) {
	var result Receipt
	path, err := mediaPath(bot)
	if err != nil || !spec.valid() {
		return result, ErrInvalid
	}
	body, _ := json.Marshal(spec)
	err = c.json(ctx, http.MethodPost, path+"/uploads", body, http.Header{"Content-Type": {"application/json"}}, 201, &result)
	return result, err
}

func (c *Client) receipt(ctx context.Context, method, bot, id, suffix string, body []byte, headers http.Header) (Receipt, error) {
	var result Receipt
	path, err := mediaPath(bot)
	if err != nil || !validID(id) {
		return result, ErrInvalid
	}
	err = c.json(ctx, method, path+"/uploads/"+id+suffix, body, headers, 200, &result)
	return result, err
}

func (c *Client) Get(ctx context.Context, bot, id string) (Receipt, error) {
	return c.receipt(ctx, http.MethodGet, bot, id, "", nil, nil)
}

func (c *Client) Patch(ctx context.Context, bot, id string, offset int64, body []byte) (Receipt, error) {
	if offset < 0 || len(body) == 0 || len(body) > MaxChunkBytes {
		return Receipt{}, ErrInvalid
	}
	sum := sha256.Sum256(body)
	return c.receipt(ctx, http.MethodPatch, bot, id, "", body, http.Header{
		"Content-Type":  {"application/octet-stream"},
		"Upload-Offset": {strconv.FormatInt(offset, 10)},
		"Upload-Length": {strconv.Itoa(len(body))},
		"Upload-Sha256": {hex.EncodeToString(sum[:])},
	})
}

func (c *Client) Complete(ctx context.Context, bot, id string) (Receipt, error) {
	return c.receipt(ctx, http.MethodPost, bot, id, "/complete", nil, nil)
}

func (c *Client) Cancel(ctx context.Context, bot, id string) (Receipt, error) {
	return c.receipt(ctx, http.MethodDelete, bot, id, "", nil, nil)
}
