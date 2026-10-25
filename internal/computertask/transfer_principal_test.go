package computertask

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/db"
)

func principalFixture(t *testing.T, direction string) (*deliveryFixture, *DeliveryService, context.Context, VerifiedTransfer) {
	t.Helper()
	f, service, _, ref := newDeliveryFixture(t)
	ref.Direction = direction
	now := time.Now()
	p := VerifiedTransfer{Identity: f.id, DeliveryID: "file-one", Reference: ref, Audience: TransferAudience, CredentialID: "synthetic-grant", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(10 * time.Minute)}
	ctx, err := BindVerifiedTransfer(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	return f, service, ctx, p
}

// This tests the provider-independent verified-principal boundary only. The
// transaction fixture still resolves the existing Agenvo execution target.
func TestTransferPrincipalReplacementPreservesReceiptAndScope(t *testing.T) {
	for _, mapping := range []string{"legacy", "native-projection"} {
		t.Run(mapping, func(t *testing.T) { exerciseTransferPrincipalReplacement(t, mapping == "native-projection") })
	}
}

func exerciseTransferPrincipalReplacement(t *testing.T, native bool) {
	t.Helper()
	f, service, _, p := principalFixture(t, "upload")
	f.target.ConnectionID = uuid.NewString()
	if native {
		// Agreed native projection: both connection and device identify the
		// registered runtime UUID, not its transient connection or mrk token.
		f.target.DeviceID = f.target.ConnectionID
		f.id.DeviceID = f.target.DeviceID
		p.Identity = f.id
	}
	f.record.TargetFingerprint = agenvo.TargetFingerprint(f.target)
	p.Device = VerifiedTransferDevice{CredentialID: p.CredentialID, ConnectionID: f.target.ConnectionID, DeviceID: f.target.DeviceID, InstanceID: f.target.InstanceID}
	bind := func(p VerifiedTransfer) context.Context {
		t.Helper()
		ctx, err := BindVerifiedTransfer(t.Context(), p)
		if err != nil {
			t.Fatal(err)
		}
		return ctx
	}
	ctx := bind(p)
	if err := service.ReserveTransfer(ctx); err != nil {
		t.Fatal(err)
	}
	if id, err := service.ClaimTransferUpload(ctx); err != nil || id != "" {
		t.Fatal("initial claim", id, err)
	}
	receipt := uuid.NewString()
	if err := service.FinishTransferUpload(ctx, receipt); err != nil {
		t.Fatal(err)
	}
	check := func(ctx context.Context, id DeliveryIdentity) error {
		if id != f.id {
			return ErrDeliveryBinding
		}
		return ctx.Err()
	}
	async, err := NewAsyncDeliveryService(t.Context(), service, check, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(async.Close)
	if out, err := async.Commit(ctx, p.Identity.OperationID, p.DeliveryID, p.Reference, ""); err != nil || out.Outcome != "pending" {
		t.Fatal(out, err)
	}
	async.wg.Wait()
	awaitDelivery(ctx, t, async, p.Identity.OperationID, p.DeliveryID, p.Reference, "committed")
	async.Close()
	revision, details := f.record.Revision, string(f.record.Details)
	// A credential ID is authentication, not a new delivery namespace. An
	// externally verified replacement for the SAME authorized tuple must keep
	// the original operation/attempt/reference/receipt; no token is issued here.
	p.CredentialID = "synthetic-replacement"
	p.Device.CredentialID = p.CredentialID
	reopened := NewDeliveryService(f.transact, f)
	ctx = bind(p)
	if got, err := reopened.TransferUploadID(ctx); err != nil || got != receipt {
		t.Fatal("reconstructed principal lost the original receipt", got, err)
	}
	reopenedAsync, err := NewAsyncDeliveryService(t.Context(), reopened, check, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopenedAsync.Close)
	if out, err := reopenedAsync.Resolve(ctx, p.Identity.OperationID, p.DeliveryID, p.Reference, "", false); err != nil || out.Outcome != "committed" {
		t.Fatal("original delivery no longer resolves", out, err)
	}
	for _, field := range []string{"connection", "device", "instance", "target-fingerprint"} {
		t.Run(field, func(t *testing.T) {
			changed, originalTarget := p, f.target
			want := ErrDenied
			switch field {
			case "connection":
				changed.Device.ConnectionID = uuid.NewString()
			case "device":
				changed.Device.DeviceID = "other-device"
			case "instance":
				changed.Device.InstanceID = "other-instance"
			case "target-fingerprint":
				f.target.Session = "different-execution-target"
				want = ErrDeliveryBinding
			}
			if _, err := reopened.TransferUploadID(bind(changed)); !errors.Is(err, want) {
				t.Fatal("another authorized identity inherited the receipt", err)
			}
			f.target = originalTarget
		})
	}
	if f.record.Revision != revision || string(f.record.Details) != details || f.verifications != 1 {
		t.Fatal("reopening/denying a principal changed the ledger or reprocessed the file")
	}
}

func TestTransferPrincipalReserveAndReceiptCAS(t *testing.T) {
	f, service, ctx, p := principalFixture(t, "upload")
	f.conflicts = 1
	if err := service.ReserveTransfer(ctx); err != nil {
		t.Fatal(err)
	}
	revision := f.record.Revision
	if err := service.ReserveTransfer(ctx); err != nil || f.record.Revision != revision {
		t.Fatal("repeated grant changed the operation", err)
	}
	if id, err := service.ClaimTransferUpload(ctx); err != nil || id != "" {
		t.Fatal(id, err)
	}
	if _, err := service.ClaimTransferUpload(ctx); !errors.Is(err, ErrTransferCreateUnknown) {
		t.Fatal("unknown create was reissued", err)
	}
	id := uuid.NewString()
	f.loseResponse = db.ErrCommitOutcomeUnknown
	if err := service.FinishTransferUpload(ctx, id); !errors.Is(err, db.ErrCommitOutcomeUnknown) {
		t.Fatal(err)
	}
	if got, err := service.ClaimTransferUpload(ctx); err != nil || got != id {
		t.Fatal("lost CAS acknowledgement changed receipt", got, err)
	}
	if err := service.FinishTransferUpload(ctx, uuid.NewString()); !errors.Is(err, ErrDeliveryConflict) {
		t.Fatal("receipt replacement accepted", err)
	}
	changed := p
	changed.Reference.SizeBytes++
	other, err := BindVerifiedTransfer(t.Context(), changed)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ReserveTransfer(other); !errors.Is(err, ErrDeliveryConflict) {
		t.Fatal("grant changed immutable file", err)
	}
	// New delivery gets no authority over the old receipt even for identical bytes.
	changed = p
	changed.DeliveryID = "file-two"
	other, err = BindVerifiedTransfer(t.Context(), changed)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ReserveTransfer(other); err != nil {
		t.Fatal(err)
	}
	if got, err := service.TransferUploadID(other); err != nil || got != "" {
		t.Fatal("another delivery inherited receipt", got, err)
	}
	var details struct {
		Ledger deliveryLedger `json:"delivery_ledger"`
	}
	if err := json.Unmarshal(f.record.Details, &details); err != nil || details.Ledger.Version != transferLedgerVersion {
		t.Fatal("missing version barrier", err)
	}
	// The old v2 writer cannot erase the new binding.
	if err := f.transact(ctx, func(tx DeliveryTransaction) error {
		_, err := mutateDeliveryTransaction(ctx, tx, f.id, p.DeliveryID, p.Reference, 2, func(*deliveryEntry, bool) (bool, error) { return true, nil })
		return err
	}); !errors.Is(err, ErrDeliveryVersion) {
		t.Fatal("old writer accepted the new ledger", err)
	}
}

func TestTransferPrincipalScopeAndLifetime(t *testing.T) {
	for _, field := range []string{"audience", "expired", "future", "long-lived", "device", "instance", "operation", "attempt", "generation", "session"} {
		t.Run(field, func(t *testing.T) {
			_, service, _, p := principalFixture(t, "download")
			switch field {
			case "audience":
				p.Audience = "account"
			case "expired":
				p.ExpiresAt = time.Now().Add(-time.Second)
			case "future":
				p.IssuedAt = time.Now().Add(time.Minute)
			case "long-lived":
				p.ExpiresAt = p.IssuedAt.Add(time.Hour)
			case "device":
				p.Identity.DeviceID = "other"
			case "instance":
				p.Identity.InstanceID = "other"
			case "operation":
				p.Identity.OperationID = uuid.NewString()
			case "attempt":
				p.Identity.Attempt = "ffffffffffffffffffffffffffffffff"
			case "generation":
				p.Identity.Generation = "ffffffffffffffffffffffffffffffff"
			case "session":
				p.Identity.Binding.SessionID = uuid.NewString()
			}
			ctx, err := BindVerifiedTransfer(t.Context(), p)
			if err == nil {
				err = service.ReserveTransfer(ctx)
			}
			if err == nil {
				t.Fatal("unbound principal accepted")
			}
		})
	}
}

func TestTransferPrincipalAsyncReservedAndRevocation(t *testing.T) {
	f, service, ctx, p := principalFixture(t, "download")
	if err := service.ReserveTransfer(ctx); err != nil {
		t.Fatal(err)
	}
	async, err := NewAsyncDeliveryService(t.Context(), service, func(context.Context, DeliveryIdentity) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer async.Close()
	out, err := async.Resolve(ctx, p.Identity.OperationID, p.DeliveryID, p.Reference, "", false)
	if err != nil || out.Outcome != "absent" || out.Version != 3 {
		t.Fatal("reserve presented as delivery", out, err)
	}
	out, err = async.Resolve(ctx, p.Identity.OperationID, p.DeliveryID, p.Reference, "fenced", true)
	if err != nil || out.Outcome != "canceled" {
		t.Fatal("local fence not retained", out, err)
	}
	if err := service.CheckTransfer(ctx, true); !errors.Is(err, ErrDeliveryBinding) {
		t.Fatal("canceled download readable", err)
	}
	f.revoked = true
	if err := service.CheckTransfer(ctx, false); !errors.Is(err, ErrDenied) {
		t.Fatal("fresh permission was not checked", err)
	}
}
