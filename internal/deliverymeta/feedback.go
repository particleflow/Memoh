package deliverymeta

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"path"
	"time"
)

const (
	ProducerContextName  = "producer-context.json"
	ProducerFeedbackName = "delivery-feedback.json"
	FeedbackMaxBytes     = 8192
)

// AttemptDirectory is the existing layout shared by execution receipt writers,
// readers and the advisory delivery projection. It adds no root/configuration.
func AttemptDirectory(attempt string) (string, bool) {
	if !hex32.MatchString(attempt) {
		return "", false
	}
	return path.Join(".memoh-computer", "attempts", attempt), true
}

type Feedback struct {
	Version  int            `json:"version"`
	Task     Task           `json:"task"`
	PlanHash string         `json:"plan_hash"`
	Files    []FileFeedback `json:"files"`
}

// This receipt cannot report delivered or contain content/credentials/logs.
// NAS ledger remains the sole authority for successful association.
type FileFeedback struct {
	DeliveryID string    `json:"delivery_id"`
	State      string    `json:"state"`
	Code       string    `json:"code"`
	Retryable  bool      `json:"retryable"`
	RetryAt    time.Time `json:"retry_at,omitempty"`
	Change     string    `json:"change"`
}

func PlanHash(p Plan) string {
	raw, _ := json.Marshal(p)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (f Feedback) Hash() string {
	raw, _ := json.Marshal(f)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (f FileFeedback) Valid() bool {
	if !hex64.MatchString(f.DeliveryID) || !hex64.MatchString(f.Change) {
		return false
	}
	switch f.State {
	case "pending":
		if !f.Retryable {
			return false
		}
		switch f.Code {
		case "queued", "uploading", "admission_pending", "association_pending", "retry_backoff", "capacity_wait", "queue_busy", "queue_unavailable":
			return true
		}
	case "blocked":
		if f.Retryable || !f.RetryAt.IsZero() {
			return false
		}
		switch f.Code {
		case "authorization", "protocol", "receipt_unavailable", "input_or_checksum", "file_limit", "retry_budget", "receipt_mismatch", "resumable_unavailable", "snapshot_unavailable", "source_changed", "source_missing", "configuration_changed", "capacity_blocked", "manifest_invalid":
			return true
		}
	case "unknown":
		if f.Retryable || !f.RetryAt.IsZero() {
			return false
		}
		switch f.Code {
		case "receipt_unavailable", "create_outcome_unknown", "association_requires_reconciliation", "closure_or_admission_unknown", "local_canceled", "awaiting_nas_ledger":
			return true
		}
	}
	return false
}

func (f Feedback) ValidFor(p Plan) bool {
	if f.Version != 1 || !p.Valid() || f.Task != p.Task || f.PlanHash != PlanHash(p) {
		return false
	}
	plans := p.FilePlans()
	if len(f.Files) != len(plans) {
		return false
	}
	for i, file := range f.Files {
		if !file.Valid() || file.DeliveryID != plans[i].DeliveryID() {
			return false
		}
	}
	return true
}

func DecodeFeedback(raw []byte, p Plan) (*Feedback, bool) {
	if len(raw) == 0 || len(raw) >= FeedbackMaxBytes {
		return nil, false
	}
	var f Feedback
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&f) != nil || dec.Decode(new(any)) != io.EOF || !f.ValidFor(p) {
		return nil, false
	}
	return &f, true
}
