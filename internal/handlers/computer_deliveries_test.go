package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
	"github.com/felinics/memoh/internal/toolcontext"
)

type deliveryHTTPFixture struct {
	dbstore.ComputerOperationStore
	r       dbstore.ComputerOperationRecord
	id      computertask.DeliveryIdentity
	target  agenvo.Target
	ref     computertask.DeliveryRequest
	trusted bool
	revoked bool
	lost    error
	calls   int
}

func (f *deliveryHTTPFixture) ReadComputerOperation(context.Context, dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
	return f.r, nil
}

func (f *deliveryHTTPFixture) SaveComputerDelivery(_ context.Context, r dbstore.ComputerOperationRecord, data dbstore.JSON) (dbstore.ComputerOperationRecord, error) {
	if r.Revision != f.r.Revision {
		return r, db.ErrNotFound
	}
	var d map[string]json.RawMessage
	_ = json.Unmarshal(r.Details, &d)
	d["delivery_ledger"] = json.RawMessage(data)
	r.Details, _ = json.Marshal(d)
	r.Revision++
	f.r = r
	return r, nil
}

func deliveryHTTPServer(t *testing.T) (*deliveryHTTPFixture, *echo.Echo) {
	t.Helper()
	f := &deliveryHTTPFixture{trusted: true}
	b := toolcontext.ComputerBinding{BotID: uuid.NewString(), UserID: uuid.NewString(), SessionID: uuid.NewString(), TargetID: uuid.NewString(), RunID: "fixture-run"}
	f.id = computertask.DeliveryIdentity{Binding: b, OperationID: uuid.NewString(), DeviceID: "fixture-device", InstanceID: "fixture-instance", Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32)}
	f.target = agenvo.Target{DeviceID: f.id.DeviceID, InstanceID: f.id.InstanceID, Agent: agenvo.AgentProfile{ExecutionMode: "pi-json-v1"}}
	details, _ := json.Marshal(computertask.Details{Kind: "herdr_pi_task", Batch: &computertask.BatchDetails{Progress: agenvo.BatchProgress{Attempt: f.id.Attempt, Generation: f.id.Generation}}})
	f.r = dbstore.ComputerOperationRecord{ID: f.id.OperationID, BotID: b.BotID, SessionID: b.SessionID, UserID: b.UserID, TargetID: b.TargetID, RunID: b.RunID, TargetFingerprint: agenvo.TargetFingerprint(f.target), State: "submitted", Revision: 1, Details: details}
	assets := media.NewService(nil, localfs.NewOriginals(t.TempDir()))
	assets.SetRequirePersistence(true)
	asset, err := assets.Ingest(t.Context(), media.IngestInput{BotID: b.BotID, Mime: "text/plain", Reader: strings.NewReader("fixture"), MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	f.ref = computertask.DeliveryRequest{Version: 1, Direction: "upload", ContentHash: asset.ContentHash, SizeBytes: asset.SizeBytes, Mime: asset.Mime}
	s := computertask.NewDeliveryService(func(_ context.Context, fn func(computertask.DeliveryTransaction) error) error {
		f.calls++
		if f.revoked {
			return computertask.ErrDenied
		}
		if err := fn(computertask.DeliveryTransaction{Store: f, Authorize: func(context.Context, computertask.DeliveryIdentity) (agenvo.Target, error) { return f.target, nil }}); err != nil {
			return err
		}
		err := f.lost
		f.lost = nil
		return err
	}, assets)
	e := echo.New()
	e.HTTPErrorHandler = server.NewHTTPErrorHandler(slog.New(slog.DiscardHandler))
	NewComputerDeliveryHandler(s).Register(e)
	// Explicit fixture for the missing authenticated host adapter. Headers are
	// intentionally ignored; neither a JWT nor an operation ID binds a host.
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if f.trusted {
				ctx, err := toolcontext.WithComputerBinding(c.Request().Context(), b)
				if err != nil {
					return err
				}
				ctx, err = computertask.WithDeliveryIdentity(ctx, f.id)
				if err != nil {
					return err
				}
				c.SetRequest(c.Request().WithContext(ctx))
			}
			return next(c)
		}
	})
	return f, e
}

func deliveryHTTPRequest(t *testing.T, e *echo.Echo, method, path, body string, want int) string {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer synthetic-not-authority")
	r.Header.Set("X-Operation-ID", taskOp)
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, r)
	if rr.Code != want || rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status/cache: %d %s body=%s", rr.Code, rr.Header().Get("Cache-Control"), rr.Body.String())
	}
	return rr.Body.String()
}

func TestComputerDeliveryHTTPUnknownThenCancel(t *testing.T) {
	for _, lost := range []error{context.DeadlineExceeded, context.Canceled} {
		t.Run(lost.Error(), func(t *testing.T) {
			f, e := deliveryHTTPServer(t)
			path := "/internal/computer-deliveries/" + f.id.OperationID + "/result/"
			body, _ := json.Marshal(f.ref)
			f.lost = lost
			deliveryHTTPRequest(t, e, "POST", path+"commit", string(body), 503)
			cancelBody := strings.TrimSuffix(string(body), "}") + `,"cancel":true}`
			response := deliveryHTTPRequest(t, e, "POST", path+"resolve", cancelBody, 200)
			if !strings.Contains(response, `"outcome":"committed"`) || f.r.Revision != 2 || f.r.CancelRequested {
				t.Fatalf("canceled a committed delivery: %s", response)
			}
			f.revoked = true
			response = deliveryHTTPRequest(t, e, "POST", path+"resolve", string(body), 403)
			if strings.Contains(response, "committed") || strings.Contains(response, f.ref.ContentHash) {
				t.Fatal("revoked outcome leaked")
			}
		})
	}
}

func TestComputerDeliveryHTTPRejectsUntrustedAndMalformedRequests(t *testing.T) {
	f, e := deliveryHTTPServer(t)
	path := "/internal/computer-deliveries/" + f.id.OperationID + "/result/"
	body, _ := json.Marshal(f.ref)
	f.trusted = false
	deliveryHTTPRequest(t, e, "POST", path+"commit", string(body), 403)
	if f.calls != 0 {
		t.Fatal("forged transport identity reached store")
	}
	f.trusted = true
	for _, bad := range []string{`{}`, string(body) + `{}`, strings.TrimSuffix(string(body), "}") + `,"operation_id":"forged"}`, strings.Repeat(" ", 5000) + string(body)} {
		deliveryHTTPRequest(t, e, "POST", path+"commit", bad, 400)
	}
	deliveryHTTPRequest(t, e, "GET", path+"commit", "", 405)
	deliveryHTTPRequest(t, e, "POST", path+"unknown", "{}", 404)
	deliveryHTTPRequest(t, e, "POST", strings.Replace(path, f.id.OperationID, uuid.NewString(), 1)+"resolve", string(body), 403)
	e.Use(func(echo.HandlerFunc) echo.HandlerFunc {
		return func(echo.Context) error { return echo.ErrUnauthorized }
	})
	deliveryHTTPRequest(t, e, "POST", path+"commit", string(body), 401)
}

func TestComputerDeliveryHTTPSFixture(t *testing.T) {
	f, e := deliveryHTTPServer(t)
	ts := httptest.NewTLSServer(e)
	defer ts.Close()
	body, _ := json.Marshal(f.ref)
	// Client trusts only the isolated fixture certificate; verification remains on.
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.URL+"/internal/computer-deliveries/"+f.id.OperationID+"/tls-result/commit", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Client().Do(req) //nolint:gosec // URL is the isolated httptest TLS server, never caller input.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK || resp.TLS == nil || resp.Header.Get("Cache-Control") != "no-store" || !strings.Contains(string(data), `"committed"`) {
		t.Fatalf("TLS fixture: %d %s %v", resp.StatusCode, data, err)
	}
}

func TestAsyncComputerDeliveryHTTPRequiresBoundBot(t *testing.T) {
	f, e := deliveryHTTPServer(t)
	assets := media.NewService(nil, localfs.NewOriginals(t.TempDir()))
	base := computertask.NewDeliveryService(func(context.Context, func(computertask.DeliveryTransaction) error) error {
		f.calls++
		return computertask.ErrDenied
	}, assets)
	async, err := computertask.NewAsyncDeliveryService(t.Context(), base, func(context.Context, computertask.DeliveryIdentity) error { return nil }, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	defer async.Close()
	NewAsyncComputerDeliveryHandler(async).Register(e)
	path := "/internal/computer-deliveries/" + f.id.OperationID + "/bound/"
	body, _ := json.Marshal(f.ref)
	for _, bot := range []string{"", uuid.NewString(), f.id.Binding.BotID} {
		request := strings.TrimSuffix(string(body), "}") + `,"bot_id":"` + bot + `"}`
		before := f.calls
		deliveryHTTPRequest(t, e, "POST", path+"commit", request, 403)
		if (f.calls > before) != (bot == f.id.Binding.BotID) {
			t.Fatal("mismatched Bot reached store")
		}
	}
}
