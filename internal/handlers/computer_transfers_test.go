package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
	"github.com/felinics/memoh/internal/transferclient"
)

type transferVerifierFixture struct {
	mu      sync.Mutex
	grants  map[string]computertask.VerifiedTransfer
	revoked atomic.Bool
	calls   atomic.Int64
}

func (v *transferVerifierFixture) Verify(_ context.Context, credential string) (computertask.VerifiedTransfer, error) {
	v.calls.Add(1)
	v.mu.Lock()
	defer v.mu.Unlock()
	p, ok := v.grants[credential]
	if !ok {
		return p, computertask.ErrDenied
	}
	return p, nil
}

func (v *transferVerifierFixture) Revalidate(ctx context.Context, _ computertask.VerifiedTransfer) error {
	if v.revoked.Load() {
		return computertask.ErrDenied
	}
	return ctx.Err()
}

type taskTransferFixtureOptions struct {
	assets  func(*media.Service) computertask.DeliveryAssets
	request func(*http.Request)
	device  func(computertask.DeliveryIdentity, agenvo.Target) DeviceTransferVerifier
}

func taskTransferHTTPFixture(t *testing.T, disabled bool, options ...taskTransferFixtureOptions) (*deliveryHTTPFixture, *transferVerifierFixture, *httptest.Server, *transferclient.Client) {
	t.Helper()
	f, _ := deliveryHTTPServer(t)
	root := t.TempDir()
	assets := media.NewService(nil, localfs.NewOriginals(root))
	assets.SetRequirePersistence(true)
	if err := assets.EnableUploads(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = assets.Uploads().Close() })
	var option taskTransferFixtureOptions
	if len(options) > 0 {
		option = options[0]
	}
	var verificationAssets computertask.DeliveryAssets = assets
	if option.assets != nil {
		verificationAssets = option.assets(assets)
	}
	var mu sync.Mutex
	base := computertask.NewDeliveryService(func(_ context.Context, apply func(computertask.DeliveryTransaction) error) error {
		mu.Lock()
		defer mu.Unlock()
		f.calls++
		before := f.r
		err := apply(computertask.DeliveryTransaction{Store: f, Authorize: func(context.Context, computertask.DeliveryIdentity) (agenvo.Target, error) { return f.target, nil }})
		if err != nil {
			f.r = before
		}
		return err
	}, verificationAssets)
	p := computertask.VerifiedTransfer{Identity: f.id, DeliveryID: "one", Reference: f.ref, Audience: computertask.TransferAudience, CredentialID: "fixture-one", IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(10 * time.Minute)}
	v := &transferVerifierFixture{grants: map[string]computertask.VerifiedTransfer{"one": p}}
	second := p
	second.DeliveryID, second.CredentialID = "two", "fixture-two"
	v.grants["two"] = second
	var verifier ExecutionTransferVerifier = v
	if disabled {
		verifier = nil
	}
	log := slog.New(slog.DiscardHandler)
	h, err := NewComputerTransferHandler(t.Context(), base, &MessageHandler{mediaService: assets}, verifier, log)
	if option.device != nil {
		h.Close()
		h, err = NewDeviceComputerTransferHandler(t.Context(), base, &MessageHandler{mediaService: assets}, option.device(f.id, f.target), log)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	e := echo.New()
	e.HTTPErrorHandler = server.NewHTTPErrorHandler(log)
	h.Register(e)
	if option.request != nil {
		e.Pre(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				option.request(c.Request())
				return next(c)
			}
		})
	}
	ts := httptest.NewTLSServer(e)
	t.Cleanup(ts.Close)
	client, err := transferclient.NewTask(ts.URL, f.id.OperationID, "one", ts.Client(), func(context.Context) (string, error) { return "Memoh-Transfer one", nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.CloseIdleConnections)
	return f, v, ts, client
}

func TestTaskTransferHTTPUploadAndReceiptIsolation(t *testing.T) {
	f, verifier, https, client := taskTransferHTTPFixture(t, false)
	spec := transferclient.Spec{SizeBytes: f.ref.SizeBytes, SHA256: f.ref.ContentHash, Mime: f.ref.Mime}
	created, err := client.Create(t.Context(), f.id.Binding.BotID, spec)
	if err != nil {
		t.Fatal(err)
	}
	// Repeated admission reuses the persisted receipt, without consuming quota.
	again, err := client.Create(t.Context(), f.id.Binding.BotID, spec)
	if err != nil || again.ID != created.ID {
		t.Fatal("create rebound receipt", err)
	}
	other, err := transferclient.NewTask(https.URL, f.id.OperationID, "two", https.Client(), func(context.Context) (string, error) { return "Memoh-Transfer two", nil })
	if err != nil {
		t.Fatal(err)
	}
	defer other.CloseIdleConnections()
	_, err = other.Get(t.Context(), f.id.Binding.BotID, created.ID)
	var status *transferclient.HTTPError
	if !errors.As(err, &status) || status.Status != 403 {
		t.Fatal("same-account same-hash receipt crossed delivery", err)
	}
	if receipt, err := client.Patch(t.Context(), f.id.Binding.BotID, created.ID, 0, []byte("fixture")); err != nil || receipt.Offset != spec.SizeBytes || !receipt.ExpiresAt.After(created.ExpiresAt) {
		t.Fatal(receipt, err)
	}
	if receipt, err := client.Complete(t.Context(), f.id.Binding.BotID, created.ID); err != nil || receipt.State != "ready" || receipt.ContentHash != spec.SHA256 {
		t.Fatal(receipt, err)
	}
	verifier.revoked.Store(true)
	_, err = client.Get(t.Context(), f.id.Binding.BotID, created.ID)
	if !errors.As(err, &status) || status.Status != 403 {
		t.Fatal("revoked principal read ready receipt", err)
	}
}

func TestTaskTransferHTTPNoAccountFallbackAndScope(t *testing.T) {
	for _, mode := range []string{"disabled", "account-jwt", "operation", "delivery", "bot", "file", "direction", "expired", "missing", "routing"} {
		t.Run(mode, func(t *testing.T) {
			f, v, https, _ := taskTransferHTTPFixture(t, mode == "disabled")
			path := ComputerTransferPrefix + "/" + f.id.OperationID + "/one/bots/" + f.id.Binding.BotID + "/media/uploads"
			spec := media.UploadInput{SizeBytes: f.ref.SizeBytes, SHA256: f.ref.ContentHash, Mime: f.ref.Mime}
			credential, status := "Memoh-Transfer one", 403
			switch mode {
			case "disabled", "account-jwt":
				token, _, err := auth.GenerateToken(f.id.Binding.UserID, "synthetic-account-secret", time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				credential, status = "Bearer "+token, 401
				if mode == "disabled" {
					status = 503
				}
			case "operation":
				path = strings.Replace(path, f.id.OperationID, "wrong", 1)
			case "delivery":
				path = strings.Replace(path, "/one/", "/wrong/", 1)
			case "bot":
				path = strings.Replace(path, f.id.Binding.BotID, "wrong", 1)
			case "file":
				spec.SizeBytes++
			case "direction":
				v.mu.Lock()
				p := v.grants["one"]
				p.Reference.Direction = "download"
				v.grants["one"] = p
				v.mu.Unlock()
			case "expired":
				v.mu.Lock()
				p := v.grants["one"]
				p.ExpiresAt = time.Now().Add(-time.Second)
				v.grants["one"] = p
				v.mu.Unlock()
				status = 401
			case "missing":
				credential, status = "", 401
			case "routing":
				path, status = ComputerTransferPrefix+"/missing", 404
			}
			body, _ := json.Marshal(spec)
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, https.URL+path, strings.NewReader(string(body)))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", credential)
			req.Header.Set("Content-Type", "application/json")
			response, err := https.Client().Do(req) //nolint:gosec // Synthetic loopback TLS fixture only.
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode != status || response.Header.Get("Cache-Control") != "no-store" {
				t.Fatal("status/cache", response.StatusCode, response.Header.Get("Cache-Control"))
			}
			if (mode == "disabled" || mode == "account-jwt") && v.calls.Load() != 0 {
				t.Fatal("account token reached task verifier")
			}
		})
	}
}

func TestTaskTransferHTTPOriginalRangeAndRevalidation(t *testing.T) {
	f, verifier, https, client := taskTransferHTTPFixture(t, false)
	spec := transferclient.Spec{SizeBytes: f.ref.SizeBytes, SHA256: f.ref.ContentHash, Mime: f.ref.Mime}
	r, err := client.Create(t.Context(), f.id.Binding.BotID, spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Patch(t.Context(), f.id.Binding.BotID, r.ID, 0, []byte("fixture")); err != nil {
		t.Fatal(err)
	}
	if _, err = client.Complete(t.Context(), f.id.Binding.BotID, r.ID); err != nil {
		t.Fatal(err)
	}
	verifier.mu.Lock()
	principal := verifier.grants["one"]
	principal.Reference.Direction = "download"
	verifier.grants["one"] = principal
	verifier.mu.Unlock()
	for _, tc := range []struct {
		name, byteRange, wantRange, wantBody string
		status                               int
	}{
		{"range", "bytes=1-3", "bytes 1-3/7", "ixt", 206},
		{"unsatisfiable", "bytes=7-", "bytes */7", "", 416},
		{"size-mismatch", "bytes=1-3", "", "", 409},
		{"hash-mismatch", "bytes=1-3", "", "", 403},
		{"revoked", "bytes=1-3", "", "", 403},
		{"anonymous", "bytes=1-3", "", "", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verifier.mu.Lock()
			p := principal
			// Download authority has its own delivery identity; an upload receipt
			// cannot be rebound to a different direction or declared file size.
			p.DeliveryID = tc.name
			if tc.name == "size-mismatch" {
				p.Reference.SizeBytes++
			}
			if tc.name == "hash-mismatch" {
				p.Reference.ContentHash = strings.Repeat("a", 64)
			}
			verifier.grants["one"] = p
			verifier.mu.Unlock()
			verifier.revoked.Store(tc.name == "revoked")
			path := ComputerTransferPrefix + "/" + f.id.OperationID + "/" + p.DeliveryID + "/bots/" + f.id.Binding.BotID + "/media/" + f.ref.ContentHash
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, https.URL+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.name != "anonymous" {
				req.Header.Set("Authorization", "Memoh-Transfer one")
			}
			req.Header.Set("Range", tc.byteRange)
			response, err := https.Client().Do(req) //nolint:gosec // Synthetic loopback TLS fixture only.
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = response.Body.Close() }()
			body, err := io.ReadAll(response.Body)
			if err != nil || response.StatusCode != tc.status || response.Header.Get("Content-Range") != tc.wantRange || response.Header.Get("Cache-Control") != "no-store" {
				t.Fatal("range/status/cache", response.StatusCode, response.Header, err)
			}
			if tc.wantBody != "" && string(body) != tc.wantBody {
				t.Fatal("wrong original bytes")
			}
		})
	}
}

func TestTaskTransferHTTPPreservesAccountUploadQuota(t *testing.T) {
	f, verifier, https, _ := taskTransferHTTPFixture(t, false)
	var clients []*transferclient.Client
	var receipts []transferclient.Receipt
	spec := transferclient.Spec{SizeBytes: f.ref.SizeBytes, SHA256: f.ref.ContentHash, Mime: f.ref.Mime}
	for i := range 5 {
		key := "quota-" + strconv.Itoa(i)
		verifier.mu.Lock()
		p := verifier.grants["one"]
		p.DeliveryID, p.CredentialID = key, key
		verifier.grants[key] = p
		verifier.mu.Unlock()
		client, err := transferclient.NewTask(https.URL, f.id.OperationID, key, https.Client(), func(context.Context) (string, error) { return "Memoh-Transfer " + key, nil })
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(client.CloseIdleConnections)
		clients = append(clients, client)
		r, err := client.Create(t.Context(), f.id.Binding.BotID, spec)
		if i < 4 {
			if err != nil {
				t.Fatal(err)
			}
			receipts = append(receipts, r)
		} else {
			var status *transferclient.HTTPError
			if !errors.As(err, &status) || status.Status != 429 {
				t.Fatal("per-delivery principal bypassed per-account quota", err)
			}
		}
	}
	if _, err := clients[0].Cancel(t.Context(), f.id.Binding.BotID, receipts[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := clients[4].Create(t.Context(), f.id.Binding.BotID, spec); err != nil {
		t.Fatal("definite quota refusal left an unknown-create fence", err)
	}
}
