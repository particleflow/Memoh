package computertask

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/media"
)

func readyProofFixture(t *testing.T) (*deliveryFixture, *DeliveryService, context.Context, VerifiedTransfer, media.Upload) {
	t.Helper()
	f, service, ctx, p := principalFixture(t, "upload")
	if err := service.ReserveTransfer(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ClaimTransferUpload(ctx); err != nil {
		t.Fatal(err)
	}
	if err := service.FinishTransferUpload(ctx, "original-receipt"); err != nil {
		t.Fatal(err)
	}
	r := media.Upload{ID: "original-receipt", State: "ready", Offset: p.Reference.SizeBytes, SizeBytes: p.Reference.SizeBytes, ContentHash: p.Reference.ContentHash, Mime: p.Reference.Mime, ExpiresAt: time.Now().Add(time.Hour)}
	return f, service, ctx, p, r
}

func TestTransferReadyUnknownCASAndRestart(t *testing.T) {
	for _, lost := range []error{db.ErrCommitOutcomeUnknown, context.DeadlineExceeded, context.Canceled} {
		t.Run(lost.Error(), func(t *testing.T) {
			f, s, ctx, p, ready := readyProofFixture(t)
			f.loseResponse = lost
			if err := s.RecordTransferReady(ctx, ready); !errors.Is(err, lost) {
				t.Fatal("unknown commit acknowledged", err)
			}
			revision := f.record.Revision
			s = NewDeliveryService(f.transact, f)
			got, found, err := s.TransferReady(ctx)
			if err != nil || !found || !sameTransferReady(got, ready) || f.record.Revision != revision {
				t.Fatal("original proof not recovered", found, err)
			}
			if err := s.RecordTransferReady(ctx, ready); err != nil || f.record.Revision != revision {
				t.Fatal("replay mutated proof", err)
			}
			var details struct {
				Ledger deliveryLedger `json:"delivery_ledger"`
			}
			if json.Unmarshal(f.record.Details, &details) != nil || details.Ledger.Version != transferLedgerVersion || details.Ledger.Entries[p.DeliveryID].Outcome != "reserved" {
				t.Fatal("ready was presented as committed")
			}
			if err := f.transact(ctx, func(tx DeliveryTransaction) error {
				_, err := mutateDeliveryTransaction(ctx, tx, p.Identity, p.DeliveryID, p.Reference, 3, func(*deliveryEntry, bool) (bool, error) { return true, nil })
				return err
			}); !errors.Is(err, ErrDeliveryVersion) {
				t.Fatal("v3 writer could discard proof", err)
			}
			f.revoked = true
			if _, _, err := s.TransferReady(ctx); !errors.Is(err, ErrDenied) {
				t.Fatal("proof bypassed current authorization", err)
			}
		})
	}
}

func TestTransferReadyExactReceiptAndScope(t *testing.T) {
	for _, field := range []string{"receipt", "size", "offset", "hash", "mime", "state", "expiry"} {
		t.Run(field, func(t *testing.T) {
			f, s, ctx, _, ready := readyProofFixture(t)
			before := f.record.Revision
			switch field {
			case "receipt":
				ready.ID = "unbound-receipt"
			case "size":
				ready.SizeBytes++
			case "offset":
				ready.Offset--
			case "hash":
				ready.ContentHash = "wrong"
			case "mime":
				ready.Mime = "text/plain"
			case "state":
				ready.State = "uploading"
			case "expiry":
				ready.ExpiresAt = time.Time{}
			}
			if err := s.RecordTransferReady(ctx, ready); !errors.Is(err, ErrDeliveryBinding) || f.record.Revision != before {
				t.Fatal("unverified proof accepted", err)
			}
		})
	}
	f, s, ctx, p, ready := readyProofFixture(t)
	if _, found, err := s.TransferReady(ctx); err != nil || found || f.verifications != 0 {
		t.Fatal("content presence used as receipt proof", found, err)
	}
	if err := s.RecordTransferReady(ctx, ready); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"delivery", "attempt", "owner", "operation", "hash", "size"} {
		t.Run(field, func(t *testing.T) {
			other := p
			switch field {
			case "delivery":
				other.DeliveryID = "other-delivery"
			case "attempt":
				other.Identity.Attempt = "ffffffffffffffffffffffffffffffff"
			case "owner":
				other.Identity.Binding.UserID = "11111111-1111-4111-8111-111111111111"
			case "operation":
				other.Identity.OperationID = "11111111-1111-4111-8111-111111111111"
			case "hash":
				other.Reference.ContentHash = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
			case "size":
				other.Reference.SizeBytes++
			}
			bound, err := BindVerifiedTransfer(t.Context(), other)
			if err != nil {
				t.Fatal(err)
			}
			if _, found, err := s.TransferReady(bound); err == nil || found {
				t.Fatal("proof crossed scope")
			}
		})
	}
}

func TestTransferReadyCancellationBeforeProof(t *testing.T) {
	f, s, ctx, p, ready := readyProofFixture(t)
	async, err := NewAsyncDeliveryService(t.Context(), s, func(context.Context, DeliveryIdentity) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer async.Close()
	if out, err := async.Resolve(ctx, p.Identity.OperationID, p.DeliveryID, p.Reference, "", true); err != nil || out.Outcome != "canceled" {
		t.Fatal("cancel fence", out, err)
	}
	revision := f.record.Revision
	if err := s.RecordTransferReady(ctx, ready); !errors.Is(err, ErrDeliveryBinding) || f.record.Revision != revision {
		t.Fatal("late proof bypassed cancel", err)
	}
	if _, found, err := s.TransferReady(ctx); err != nil || found {
		t.Fatal("cancellation fabricated ready proof", err)
	}
}

func TestCommittedTransferReadyRequiresTerminalAuthorizedProof(t *testing.T) {
	f, s, ctx, p, ready := readyProofFixture(t)
	if err := s.RecordTransferReady(ctx, ready); err != nil {
		t.Fatal(err)
	}
	if _, found, err := s.CommittedTransferReady(ctx); err != nil || found {
		t.Fatal("ready alone authorized retirement", found, err)
	}
	async, err := NewAsyncDeliveryService(t.Context(), s, func(context.Context, DeliveryIdentity) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer async.Close()
	if _, err := async.Commit(ctx, p.Identity.OperationID, p.DeliveryID, p.Reference, ""); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, err := async.Resolve(ctx, p.Identity.OperationID, p.DeliveryID, p.Reference, "", false)
		if err != nil {
			t.Fatal(err)
		}
		if out.Outcome == "committed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("commit did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	revision := f.record.Revision
	got, found, err := s.CommittedTransferReady(ctx)
	if err != nil || !found || !sameTransferReady(got, ready) || f.record.Revision != revision {
		t.Fatal("committed proof lost or changed", found, err)
	}
	f.revoked = true
	if _, found, err := s.CommittedTransferReady(ctx); !errors.Is(err, ErrDenied) || found {
		t.Fatal("retirement bypassed fresh authorization", found, err)
	}
}
