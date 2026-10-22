package computertask

import (
	"encoding/json"

	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/deliverymeta"
)

// ProducerResult separates execution completion from NAS delivery. Only the
// operation's committed ledger can supply a content reference; PC ready status
// and command acknowledgement cannot do so.
type ProducerResult struct {
	DeliveryID string           `json:"delivery_id"`
	SourceRef  string           `json:"source_ref"`
	State      string           `json:"state"`
	Reference  *DeliveryRequest `json:"reference,omitempty"`
}

func producerResult(r dbstore.ComputerOperationRecord, d Details) *ProducerResult {
	if d.Batch == nil || d.Batch.Progress.Producer == nil {
		return nil
	}
	p := d.Batch.Progress.Producer
	out := &ProducerResult{DeliveryID: p.DeliveryID(), SourceRef: p.Artifact.SourceRef, State: "unavailable"}
	want := deliverymeta.Task{OperationID: r.ID, BotID: r.BotID, UserID: r.UserID, SessionID: r.SessionID, TargetID: r.TargetID, RunID: r.RunID, Attempt: d.Batch.Progress.Attempt, Generation: d.Batch.Progress.Generation, RootHash: p.Task.RootHash}
	if !p.Valid() || p.Task != want || len(r.Details) > 2<<20 {
		return out
	}
	var stored struct {
		Ledger *deliveryLedger `json:"delivery_ledger"`
	}
	if json.Unmarshal(r.Details, &stored) != nil {
		return out
	}
	out.State = "pending"
	if stored.Ledger == nil {
		return out
	}
	ledger := stored.Ledger
	if ledger.Version < 1 || ledger.Version > 3 || len(ledger.Entries) > maxDeliveries || ledger.Entries == nil {
		out.State = "unavailable"
		return out
	}
	entry, ok := ledger.Entries[p.DeliveryID()]
	if !ok {
		return out
	}
	if !entry.Reference.valid() || !validDeliveryEntry(entry, ledger.Version) || !deliveryHash.MatchString(entry.Scope) || entry.At.IsZero() || entry.Reference.Direction != "upload" || entry.Reference.Mime != p.Artifact.Mime {
		out.State = "unavailable"
		return out
	}
	switch entry.Outcome {
	case "committed":
		out.State = "delivered"
		ref := entry.Reference
		out.Reference = &ref
	case "failed", "canceled":
		out.State = entry.Outcome
	}
	return out
}
