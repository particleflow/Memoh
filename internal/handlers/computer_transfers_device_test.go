package handlers

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/transferclient"
)

type deviceVerifierFixture struct {
	device      computertask.VerifiedTransferDevice
	revoked     atomic.Bool
	unavailable atomic.Bool
}

func (v *deviceVerifierFixture) VerifyDevice(ctx context.Context, secret string) (computertask.VerifiedTransferDevice, error) {
	if v.unavailable.Load() {
		return computertask.VerifiedTransferDevice{}, ErrTransferVerificationUnavailable
	}
	if secret != "synthetic-reusable-device" {
		return computertask.VerifiedTransferDevice{}, computertask.ErrDenied
	}
	return v.device, ctx.Err()
}

func (v *deviceVerifierFixture) RevalidateDevice(ctx context.Context, d computertask.VerifiedTransferDevice) error {
	if v.revoked.Load() || d != v.device {
		return computertask.ErrDenied
	}
	return ctx.Err()
}

func TestDeviceTransferCredentialReuseAndRecovery(t *testing.T) {
	v := &deviceVerifierFixture{}
	f, _, https, _ := taskTransferHTTPFixture(t, false, taskTransferFixtureOptions{device: func(_ computertask.DeliveryIdentity, target agenvo.Target) DeviceTransferVerifier {
		v.device = computertask.VerifiedTransferDevice{CredentialID: "stable-device", ConnectionID: target.ConnectionID, DeviceID: target.DeviceID, InstanceID: target.InstanceID}
		return v
	}})
	task := transferclient.DeviceTask{UserID: f.id.Binding.UserID, SessionID: f.id.Binding.SessionID, TargetID: f.id.Binding.TargetID, RunID: f.id.Binding.RunID, Attempt: f.id.Attempt, Generation: f.id.Generation}
	job := transferclient.Job{Version: 1, BotID: f.id.Binding.BotID, OperationID: f.id.OperationID, DeliveryID: "device-one", SnapshotID: "snapshot", LocatorRevision: "fixture", Phase: "queued", Spec: transferclient.Spec{SizeBytes: f.ref.SizeBytes, SHA256: f.ref.ContentHash, Mime: f.ref.Mime}}
	newClient := func(j transferclient.Job, task transferclient.DeviceTask) *transferclient.Client {
		t.Helper()
		c, err := transferclient.NewDeviceTask(https.URL, j, "upload", task, https.Client(), func(context.Context) (string, error) { return "Memoh-Transfer synthetic-reusable-device", nil })
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(c.CloseIdleConnections)
		return c
	}
	first := newClient(job, task)
	v.unavailable.Store(true)
	_, err := first.Create(t.Context(), job.BotID, job.Spec)
	var temporary *transferclient.HTTPError
	if !errors.As(err, &temporary) || temporary.Status != 503 {
		t.Fatal("verifier outage must be retryable", err)
	}
	v.unavailable.Store(false)
	r, err := first.Create(t.Context(), job.BotID, job.Spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Patch(t.Context(), job.BotID, r.ID, 0, []byte("fix")); err != nil {
		t.Fatal(err)
	}
	// A fresh client with the same device identity recovers the original receipt.
	reopened := newClient(job, task)
	again, err := reopened.Create(t.Context(), job.BotID, job.Spec)
	if err != nil || again.ID != r.ID || again.Offset != 3 {
		t.Fatal("receipt changed after reopen", again, err)
	}
	if _, err := reopened.Patch(t.Context(), job.BotID, r.ID, 3, []byte("ture")); err != nil {
		t.Fatal(err)
	}
	if ready, err := reopened.Complete(t.Context(), job.BotID, r.ID); err != nil || ready.State != "ready" {
		t.Fatal(ready, err)
	}
	job.DeliveryID = "device-two"
	second := newClient(job, task)
	r2, err := second.Create(t.Context(), job.BotID, job.Spec)
	if err != nil || r2.ID == r.ID {
		t.Fatal("multi-file device credential", r2, err)
	}
	assertDenied := func(err error) {
		t.Helper()
		var h *transferclient.HTTPError
		if !errors.As(err, &h) || h.Status != http.StatusForbidden {
			t.Fatal("expected forbidden", err)
		}
	}
	_, err = second.Get(t.Context(), job.BotID, r.ID)
	assertDenied(err)
	wrong := task
	wrong.TargetID = "00000000-0000-0000-0000-000000000001"
	_, err = newClient(job, wrong).Get(t.Context(), job.BotID, r2.ID)
	assertDenied(err)
	wrong = task
	wrong.Attempt = "00000000000000000000000000000000"
	_, err = newClient(job, wrong).Get(t.Context(), job.BotID, r2.ID)
	assertDenied(err)
	// Even valid operation selectors cannot cross the authenticated connection.
	connection := v.device.ConnectionID
	v.device.ConnectionID = "00000000-0000-0000-0000-000000000002"
	_, err = second.Get(t.Context(), job.BotID, r2.ID)
	assertDenied(err)
	v.device.ConnectionID = connection
	v.revoked.Store(true)
	_, err = reopened.Get(t.Context(), job.BotID, r.ID)
	assertDenied(err)
}
