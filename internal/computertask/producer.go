package computertask

import (
	"encoding/json"
	"time"

	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/deliverymeta"
)

// ProducerResult separates execution completion from NAS delivery. Only the
// original committed ledger supplies references; PC ready/acceptance cannot.
// Version 1 declarations retain the existing single-file response shape.
type ProducerResult struct {
	Stage       string               `json:"stage,omitempty"`
	Retryable   bool                 `json:"retryable"`
	NeedsAction bool                 `json:"needs_action"`
	RetryAt     time.Time            `json:"retry_at,omitempty"`
	DeliveryID  string               `json:"delivery_id,omitempty"`
	SourceRef   string               `json:"source_ref,omitempty"`
	State       string               `json:"state"`
	Reference   *DeliveryRequest     `json:"reference,omitempty"`
	Files       []ProducerFileResult `json:"files,omitempty"`
	Total       int                  `json:"total,omitempty"`
	Delivered   int                  `json:"delivered,omitempty"`
	Pending     int                  `json:"pending,omitempty"`
}

type ProducerFileResult struct {
	Retryable   bool      `json:"retryable"`
	NeedsAction bool      `json:"needs_action"`
	RetryAt     time.Time `json:"retry_at,omitempty"`
	DeliveryID  string    `json:"delivery_id"`
	SourceRef   string    `json:"source_ref"`
	State       string    `json:"state"`
	// Empty ledger cannot establish whether closure or admission occurred on PC.
	Stage     string           `json:"stage,omitempty"`
	Reference *DeliveryRequest `json:"reference,omitempty"`
}

func (r *ProducerResult) hasPending() bool {
	if r == nil {
		return false
	}
	if len(r.Files) == 0 {
		return r.State == "pending" || r.State == "blocked" || r.State == "unknown"
	}
	return r.Pending > 0
}

// Cancellation eligibility is distinct from automatic progress. A blocked or
// unknown delivery remains cancelable, but does not justify another wake.
func (r *ProducerResult) canAdvance(now time.Time) bool {
	if r == nil {
		return false
	}
	if len(r.Files) == 0 {
		return r.Retryable && !r.RetryAt.After(now)
	}
	for _, file := range r.Files {
		if file.Retryable && !file.RetryAt.After(now) {
			return true
		}
	}
	return false
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
	if ledger != nil && (ledger.Version < 1 || ledger.Version > transferLedgerVersion || len(ledger.Entries) > maxDeliveries || ledger.Entries == nil) {
		return out
	}
	plans := p.FilePlans()
	for i, plan := range plans {
		file := producerFile(r, plan, ledger)
		if file.State == "pending" && d.Batch.Feedback != nil && d.Batch.Feedback.ValidFor(*p) {
			local := d.Batch.Feedback.Files[i]
			file.State, file.Stage, file.Retryable, file.RetryAt = local.State, local.Code, local.Retryable, local.RetryAt
			file.NeedsAction = !local.Retryable
		}
		if p.Version == 1 {
			out.State, out.Reference = file.State, file.Reference
			out.Stage, out.Retryable, out.NeedsAction, out.RetryAt = file.Stage, file.Retryable, file.NeedsAction, file.RetryAt
			return out
		}
		out.Files = append(out.Files, file)
		if file.State == "delivered" {
			out.Delivered++
		}
		out.Retryable = out.Retryable || file.Retryable
		out.NeedsAction = out.NeedsAction || file.NeedsAction
		if file.State == "pending" || file.State == "blocked" || file.State == "unknown" {
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
		if !out.Retryable {
			out.State = "blocked"
		}
	}
	for _, file := range out.Files {
		if file.State == "unavailable" {
			out.State, out.Retryable = "unavailable", false
		}
	}
	return out
}

func producerFile(r dbstore.ComputerOperationRecord, p deliverymeta.Plan, ledger *deliveryLedger) ProducerFileResult {
	out := ProducerFileResult{DeliveryID: p.DeliveryID(), SourceRef: p.Artifact.SourceRef, State: "pending", Retryable: true, Stage: "closure_or_admission_unknown"}
	if r.CancelRequested {
		out.State, out.Stage, out.Retryable = "canceled", "", false
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
		out.State, out.Retryable = "unavailable", false
		return out
	}
	switch entry.Outcome {
	case "committed":
		out.State, out.Retryable = "delivered", false
		ref := entry.Reference
		out.Reference = &ref
	case "failed", "canceled":
		out.State, out.Retryable = entry.Outcome, false
	default:
		out.Stage = entry.Outcome
	}
	return out
}
