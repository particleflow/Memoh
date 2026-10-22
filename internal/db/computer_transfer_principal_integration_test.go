//go:build integration

package db_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/computertask"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/handlers"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/transferclient"
	"github.com/felinics/memoh/internal/userruntime"
)

// This is the only synthetic authentication port in the combined tests. No
// account JWT or trusted identity is manually injected into HTTP contexts.
type pgTransferVerifier struct {
	principal computertask.VerifiedTransfer
	device    computertask.VerifiedTransferDevice
	revoked   atomic.Bool
	denied    chan struct{}
}

func (v *pgTransferVerifier) VerifyDevice(ctx context.Context, credential string) (computertask.VerifiedTransferDevice, error) {
	if credential != "isolated-transfer-fixture" {
		return computertask.VerifiedTransferDevice{}, computertask.ErrDenied
	}
	return v.device, ctx.Err()
}

func (v *pgTransferVerifier) RevalidateDevice(ctx context.Context, device computertask.VerifiedTransferDevice) error {
	if device != v.device {
		return computertask.ErrDenied
	}
	return v.Revalidate(ctx, v.principal)
}

func pgDeviceIdentity(ctx context.Context, t *testing.T, store *postgresstore.Store, id computertask.DeliveryIdentity) computertask.VerifiedTransferDevice {
	t.Helper()
	mount, err := store.GetMount(ctx, id.Binding.BotID, id.Binding.TargetID)
	if err != nil {
		t.Fatal(err)
	}
	target, err := agenvo.DecodeTarget(mount.RuntimeProviderConfig)
	if err != nil {
		t.Fatal(err)
	}
	return computertask.VerifiedTransferDevice{CredentialID: "fixture-device", ConnectionID: target.ConnectionID, DeviceID: target.DeviceID, InstanceID: target.InstanceID}
}

func pgDeviceTask(id computertask.DeliveryIdentity) transferclient.DeviceTask {
	return transferclient.DeviceTask{UserID: id.Binding.UserID, SessionID: id.Binding.SessionID, TargetID: id.Binding.TargetID, RunID: id.Binding.RunID, Attempt: id.Attempt, Generation: id.Generation}
}

func (v *pgTransferVerifier) Verify(ctx context.Context, credential string) (computertask.VerifiedTransfer, error) {
	if credential != "isolated-transfer-fixture" {
		return computertask.VerifiedTransfer{}, computertask.ErrDenied
	}
	return v.principal, ctx.Err()
}

func (v *pgTransferVerifier) Revalidate(ctx context.Context, p computertask.VerifiedTransfer) error {
	if v.revoked.Load() || p != v.principal {
		select {
		case v.denied <- struct{}{}:
		default:
		}
		return computertask.ErrDenied
	}
	return ctx.Err()
}

func pgTransferPrincipal(id computertask.DeliveryIdentity, delivery string, ref computertask.DeliveryRequest) computertask.VerifiedTransfer {
	return computertask.VerifiedTransfer{Identity: id, DeliveryID: delivery, Reference: ref, Audience: computertask.TransferAudience, CredentialID: "isolated-transfer-fixture", IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(10 * time.Minute)}
}

func exerciseTaskTransferPrincipal(ctx context.Context, t *testing.T, store *postgresstore.Store, assets *media.Service, id computertask.DeliveryIdentity, ref computertask.DeliveryRequest) {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	p := pgTransferPrincipal(id, "principal-upload", ref)
	verifier := newRegisteredTransferVerifier(ctx, t, store, id)
	base := computertask.NewPostgresDeliveryService(store, assets)
	messages := handlers.NewMessageHandler(log, nil, nil, nil, nil)
	messages.SetMediaService(assets)
	h, err := handlers.NewDeviceComputerTransferHandler(ctx, base, messages, verifier, log)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	e := server.NewServer(log, ":0", "isolated-account-secret", h) //nolint:contextcheck // Requests supply their own contexts.
	var lose atomic.Bool
	https := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/commit") && lose.Swap(false) {
			e.ServeHTTP(httptest.NewRecorder(), r)
			w.WriteHeader(499) // Real accepted mutation, canceled acknowledgement.
			return
		}
		e.ServeHTTP(w, r)
	}))
	defer https.Close()
	job := transferclient.Job{Version: 1, DeliveryID: p.DeliveryID, OperationID: id.OperationID, BotID: id.Binding.BotID, SnapshotID: "fixture", LocatorRevision: "fixture", Phase: "queued", Spec: transferclient.Spec{SizeBytes: ref.SizeBytes, SHA256: ref.ContentHash, Mime: ref.Mime}}
	client, err := transferclient.NewDeviceTask(https.URL, job, "upload", pgDeviceTask(id), https.Client(), func(context.Context) (string, error) { return "Memoh-Transfer " + verifier.token, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	spec := transferclient.Spec{SizeBytes: ref.SizeBytes, SHA256: ref.ContentHash, Mime: ref.Mime}
	r, err := client.Create(ctx, id.Binding.BotID, spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Patch(ctx, id.Binding.BotID, r.ID, 0, []byte("synthetic delivery content")); err != nil {
		t.Fatal(err)
	}
	if ready, err := client.Complete(ctx, id.Binding.BotID, r.ID); err != nil || ready.State != "ready" {
		t.Fatal(ready, err)
	}
	op := dbstore.ComputerOperationRecord{ID: id.OperationID, BotID: id.Binding.BotID, UserID: id.Binding.UserID, SessionID: id.Binding.SessionID}
	current, err := store.ReadComputerOperation(ctx, op)
	if err != nil {
		t.Fatal(err)
	}
	current.State = "completed"
	current.Details = []byte(`{"kind":"herdr_pi_task","batch":{"progress":{"attempt":"` + id.Attempt + `","generation":"` + id.Generation + `"}}}`)
	if _, err := store.SaveComputerOperation(ctx, current); err != nil {
		t.Fatal(err)
	}
	// Current execution CAS must retain the new v3 file/receipt binding.
	if _, err := client.Get(ctx, id.Binding.BotID, r.ID); err != nil {
		t.Fatal("execution writer erased scoped receipt", err)
	}
	file := transferclient.Reference{DeliveryID: p.DeliveryID, OperationID: id.OperationID, BotID: id.Binding.BotID, ContentHash: ref.ContentHash, SizeBytes: ref.SizeBytes, Mime: ref.Mime}
	binding := client.DeliveryBinding(nil)
	lose.Store(true)
	var status *transferclient.HTTPError
	if err := binding.Commit(ctx, "upload", file, nil); !errors.As(err, &status) || status.Status != 499 {
		t.Fatal("missing lost commit", err)
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		out, err := binding.Resolve(ctx, "upload", file, false)
		if err != nil {
			t.Fatal(err)
		}
		if out == transferclient.OutcomeCommitted {
			break
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("scoped async result never committed")
		}
	}
	// A second delivery uses the identical credential, with its own immutable
	// reference/receipt. Recreate that client to model a stopped finite command.
	job.DeliveryID = "device-second-upload"
	for range 2 {
		second, err := transferclient.NewDeviceTask(https.URL, job, "upload", pgDeviceTask(id), https.Client(), func(context.Context) (string, error) { return "Memoh-Transfer " + verifier.token, nil })
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := second.Create(ctx, job.BotID, job.Spec)
		if err != nil || receipt.ID == r.ID {
			t.Fatal("second device delivery", err)
		}
		if job.ReceiptID != "" && receipt.ID != job.ReceiptID {
			t.Fatal("reopen replaced receipt")
		}
		job.ReceiptID = receipt.ID
		_, err = second.Get(ctx, job.BotID, r.ID)
		if !errors.As(err, &status) || status.Status != 403 {
			t.Fatal("cross delivery receipt", err)
		}
		second.CloseIdleConnections()
	}
	verifier.revoke(ctx, t)
	if _, err := binding.Resolve(ctx, "upload", file, false); !errors.As(err, &status) || status.Status != 401 {
		t.Fatal("revoked principal resolved outcome", err)
	}
}

// Uses the real registry verifier. Only credentials and database are synthetic.
// The notification observes denial; it cannot override authentication results.
type registeredTransferVerifier struct {
	handlers.RuntimeDeviceTransferVerifier
	token          string
	device         computertask.VerifiedTransferDevice
	owner, runtime string
	denied         chan struct{}
}

func newRegisteredTransferVerifier(ctx context.Context, t *testing.T, store *postgresstore.Store, id computertask.DeliveryIdentity) *registeredTransferVerifier {
	t.Helper()
	mount, err := store.GetMount(ctx, id.Binding.BotID, id.Binding.TargetID)
	if err != nil {
		t.Fatal(err)
	}
	credentials := userruntime.NewTransferCredentials(store, true)
	status, err := credentials.Status(ctx, mount.RuntimeUserID, mount.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	enrollment, err := credentials.Enroll(ctx, mount.RuntimeUserID, mount.RuntimeID, userruntime.ChangeTransferCredentialRequest{ExpectedID: status.CredentialID})
	if err != nil {
		t.Fatal(err)
	}
	v := &registeredTransferVerifier{RuntimeDeviceTransferVerifier: handlers.RuntimeDeviceTransferVerifier{Credentials: credentials}, token: enrollment.Credential, owner: mount.RuntimeUserID, runtime: mount.RuntimeID, denied: make(chan struct{}, 1)}
	v.device, err = v.VerifyDevice(ctx, v.token)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (v *registeredTransferVerifier) RevalidateDevice(ctx context.Context, d computertask.VerifiedTransferDevice) error {
	err := v.RuntimeDeviceTransferVerifier.RevalidateDevice(ctx, d)
	if err != nil {
		select {
		case v.denied <- struct{}{}:
		default:
		}
	}
	return err
}

func (v *registeredTransferVerifier) revoke(ctx context.Context, t *testing.T) {
	t.Helper()
	if err := v.Credentials.Revoke(ctx, v.owner, v.runtime, v.device.CredentialID); err != nil {
		t.Fatal(err)
	}
}
