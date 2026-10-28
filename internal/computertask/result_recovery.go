package computertask

import (
	"context"
	"encoding/json"

	"github.com/felinics/memoh/internal/agenvo"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/toolcontext"
)

// PrepareProducerResultRecovery is the trusted original-owner preparation seam.
// It is intentionally not a model tool or public HTTP endpoint. Callers must
// supply an explicitly selected basename and independently known original bytes.
// This only reads authorized state; empty ledger never implies upload absence.
func (s *Service) PrepareProducerResultRecovery(ctx context.Context, b toolcontext.ComputerBinding, operation, sourceRef, physicalName, hash string, size int64, hostBinding string) (deliverymeta.ResultRecovery, error) {
	if current, ok := toolcontext.HostComputerBinding(ctx); !ok || current != b {
		return deliverymeta.ResultRecovery{}, ErrDenied
	}
	r, d, err := s.load(ctx, b, operation)
	if err != nil {
		return deliverymeta.ResultRecovery{}, err
	}
	if r.State != "completed" || r.CancelRequested || d.Kind != "herdr_pi_task" || d.Batch == nil || d.Batch.Receipt == nil || d.Batch.Result == nil ||
		d.Batch.Progress.Producer == nil || d.Batch.Result.Outcome != "succeeded" || agenvo.ValidateBatchCompletionResult(*d.Batch.Result, d.Batch.Progress, *d.Batch.Receipt) != nil {
		return deliverymeta.ResultRecovery{}, ErrInvalid
	}
	p := d.Batch.Progress.Producer
	want := deliverymeta.Task{OperationID: r.ID, BotID: r.BotID, UserID: r.UserID, SessionID: r.SessionID, TargetID: r.TargetID, RunID: r.RunID, Attempt: d.Batch.Progress.Attempt, Generation: d.Batch.Progress.Generation, RootHash: p.Task.RootHash}
	if !p.Valid() || p.Task != want || d.TargetSnapshot == nil || p.Task.RootHash != deliverymeta.RootHash(d.TargetSnapshot.WorkDir) {
		return deliverymeta.ResultRecovery{}, ErrInvalid
	}
	receipt, _ := json.Marshal(d.Batch.Receipt)
	request := deliverymeta.ResultRecovery{Version: 1, Plan: *p, SourceRef: sourceRef, PhysicalName: physicalName, SHA256: hash, SizeBytes: size,
		HostBinding: hostBinding, TargetFingerprint: r.TargetFingerprint, RequestHash: d.Batch.Progress.RequestHash, OperationFingerprint: r.Fingerprint, ContextHash: deliverymeta.PlanHash(*p), Receipt: receipt, RemoteOutcome: "absent"}
	// The launch writes exactly this context encoding. Do not reinterpret an
	// already-completed request or synthesize a context from the selected name.
	file, ok := request.FilePlan()
	if !ok {
		return deliverymeta.ResultRecovery{}, ErrInvalid
	}
	entry, exists, err := recoveryDeliveryEntry(r, file.DeliveryID())
	if err != nil {
		return deliverymeta.ResultRecovery{}, err
	}
	request.RemoteOutcome = "absent"
	if exists {
		request.RemoteOutcome = "unknown"
		if entry.Reference.ContentHash != hash || entry.Reference.SizeBytes != size || entry.Reference.Mime != file.Artifact.Mime || entry.Reference.Direction != "upload" {
			return deliverymeta.ResultRecovery{}, ErrDeliveryConflict
		}
		if entry.Transfer != nil && entry.Transfer.UploadID != "" {
			request.RemoteOutcome, request.KnownReceipt = "existing", entry.Transfer.UploadID
		}
	}
	return request, nil
}

func recoveryDeliveryEntry(r dbstore.ComputerOperationRecord, id string) (deliveryEntry, bool, error) {
	var stored struct {
		Ledger *deliveryLedger `json:"delivery_ledger"`
	}
	if json.Unmarshal(r.Details, &stored) != nil {
		return deliveryEntry{}, false, ErrDeliveryBinding
	}
	if stored.Ledger != nil && stored.Ledger.Version == 5 {
		if r.DeliveryEntries == nil {
			return deliveryEntry{}, false, ErrDeliveryBinding
		}
		row, found := r.DeliveryEntries[id]
		if !found {
			return deliveryEntry{}, false, nil
		}
		var entry deliveryEntry
		if json.Unmarshal(row.Data, &entry) != nil || !validStoredDeliveryEntry(id, entry, row.Version) {
			return deliveryEntry{}, false, ErrDeliveryConflict
		}
		return entry, true, nil
	}
	if stored.Ledger == nil {
		return deliveryEntry{}, false, nil
	}
	if stored.Ledger.Version < 1 || stored.Ledger.Version >= 5 || stored.Ledger.Entries == nil {
		return deliveryEntry{}, false, ErrDeliveryVersion
	}
	entry, found := stored.Ledger.Entries[id]
	if found && !validStoredDeliveryEntry(id, entry, stored.Ledger.Version) {
		return deliveryEntry{}, false, ErrDeliveryConflict
	}
	return entry, found, nil
}

// RecoverProducerResult only dispatches the existing finite local producer
// entry through the original verified control pane. Acceptance is not mapping,
// upload or commit evidence; normal feedback/task queries establish the result.
func (s *Service) RecoverProducerResult(ctx context.Context, b toolcontext.ComputerBinding, operation, sourceRef, physicalName, hash string, size int64, hostBinding string) error {
	request, err := s.PrepareProducerResultRecovery(ctx, b, operation, sourceRef, physicalName, hash, size, hostBinding)
	if err != nil {
		return err
	}
	if request.RemoteOutcome == "unknown" {
		return ErrDeliveryBinding
	}
	r, d, err := s.load(ctx, b, operation)
	if err != nil {
		return err
	}
	if d.Batch == nil || d.Batch.Progress.Producer == nil || deliverymeta.PlanHash(*d.Batch.Progress.Producer) != deliverymeta.PlanHash(request.Plan) || r.CancelRequested || r.State != "completed" || r.Fingerprint != request.OperationFingerprint || r.TargetFingerprint != request.TargetFingerprint || d.Batch.Progress.RequestHash != request.RequestHash {
		return ErrInvalid
	}
	return s.relay.RecoverBatchDelivery(agenvo.PinRegistration(ctx, scope(b, r.TargetID), r.TargetFingerprint), scope(b, r.TargetID), d.Batch.Progress, request)
}
