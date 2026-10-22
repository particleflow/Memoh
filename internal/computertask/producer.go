package computertask

import (
	"encoding/json"

	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/deliverymeta"
)

// ProducerResult separates execution completion from NAS delivery. Only the
// original committed ledger supplies references; PC ready/acceptance cannot.
// Version 1 declarations retain the existing single-file response shape.
type ProducerResult struct {
	DeliveryID string               `json:"delivery_id,omitempty"`
	SourceRef  string               `json:"source_ref,omitempty"`
	State      string               `json:"state"`
	Reference  *DeliveryRequest     `json:"reference,omitempty"`
	Files      []ProducerFileResult `json:"files,omitempty"`
	Total      int                  `json:"total,omitempty"`
	Delivered  int                  `json:"delivered,omitempty"`
	Pending    int                  `json:"pending,omitempty"`
}

type ProducerFileResult struct {
	DeliveryID string `json:"delivery_id"`
	SourceRef  string `json:"source_ref"`
	State      string `json:"state"`
	// Empty ledger cannot establish whether closure or admission occurred on PC.
	Stage     string           `json:"stage,omitempty"`
	Reference *DeliveryRequest `json:"reference,omitempty"`
}

func (r *ProducerResult) hasPending() bool {
	if r == nil {
		return false
	}
	if len(r.Files) == 0 {
		return r.State == "pending"
	}
	return r.Pending > 0
}

func producerResult(r dbstore.ComputerOperationRecord, d Details) *ProducerResult {
	if d.Batch == nil || d.Batch.Progress.Producer == nil {
		return nil
	}
	p := d.Batch.Progress.Producer
	out := &ProducerResult{State: "unavailable"}
	if p.Version == 1 {
		out.DeliveryID, out.SourceRef = p.DeliveryID(), p.Artifact.SourceRef
	}
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
	ledger := stored.Ledger
	if ledger != nil && (ledger.Version < 1 || ledger.Version > 3 || len(ledger.Entries) > maxDeliveries || ledger.Entries == nil) {
		return out
	}
	plans := p.FilePlans()
	for _, plan := range plans {
		file := producerFile(r, plan, ledger)
		if p.Version == 1 {
			out.State, out.Reference = file.State, file.Reference
			return out
		}
		out.Files = append(out.Files, file)
		if file.State == "delivered" {
			out.Delivered++
		}
		if file.State == "pending" {
			out.Pending++
		}
	}
	out.Total = len(plans)
	out.State = "failed"
	if r.CancelRequested {
		out.State = "canceled"
	}
	switch {
	case out.Delivered == out.Total:
		out.State = "delivered"
	case out.Delivered > 0:
		out.State = "partial"
	case out.Pending > 0:
		out.State = "pending"
	}
	for _, file := range out.Files {
		if file.State == "unavailable" {
			out.State = "unavailable"
		}
	}
	return out
}

func producerFile(r dbstore.ComputerOperationRecord, p deliverymeta.Plan, ledger *deliveryLedger) ProducerFileResult {
	out := ProducerFileResult{DeliveryID: p.DeliveryID(), SourceRef: p.Artifact.SourceRef, State: "pending", Stage: "closure_or_admission_unknown"}
	if r.CancelRequested {
		out.State, out.Stage = "canceled", ""
	}
	if ledger == nil {
		return out
	}
	entry, ok := ledger.Entries[p.DeliveryID()]
	if !ok {
		return out
	}
	out.Stage = ""
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
	default:
		out.Stage = entry.Outcome
	}
	return out
}
