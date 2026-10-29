package transferclient

import (
	"context"

	"github.com/felinics/memoh/internal/deliverymeta"
)

// ReceiptBinding is a local observation of one original queue record. It is
// neither server receipt observation, upload authorization nor delivery proof.
// An empty KnownReceipt never establishes that no upload exists on the server.
type ReceiptBinding struct {
	Schema           string            `json:"schema"`
	State            string            `json:"state"`
	Scope            string            `json:"scope"`
	ServerEffects    string            `json:"server_effects"`
	DeliveryID       string            `json:"delivery_id"`
	Task             deliverymeta.Task `json:"task"`
	HostBinding      string            `json:"host_binding"`
	SourceRef        string            `json:"source_ref,omitempty"`
	ContentHash      string            `json:"content_hash"`
	SizeBytes        int64             `json:"size_bytes"`
	Mime             string            `json:"mime"`
	KnownReceipt     string            `json:"known_receipt,omitempty"`
	Phase            string            `json:"phase"`
	Associated       bool              `json:"associated"`
	AssociationState string            `json:"association_state,omitempty"`
}

// ReadReceiptBinding reuses the native locator, ACL/ownership, stripe lock,
// checksum, checkpoint, release and history readers. readOnly=true forbids
// initialization, lock-file creation, journal-tail repair, migration and sync.
// There is no credential read, source open, feedback publication or HTTP call.
func ReadReceiptBinding(ctx context.Context, getenv func(string) string, revision string, limits QueueLimits, id string) (ReceiptBinding, error) {
	if ctx == nil || !validID(id) || id == "admission" {
		return ReceiptBinding{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return ReceiptBinding{}, err
	}
	cfg, err := OpenHostConfig(getenv)
	if err != nil {
		return ReceiptBinding{}, err
	}
	defer func() { _ = cfg.Close() }()
	q, err := openHerdrQueue(getenv, revision, limits, true)
	if err != nil {
		return ReceiptBinding{}, err
	}
	defer func() { _ = q.Close() }()
	r, err := q.Inspect(id)
	if err != nil {
		return ReceiptBinding{}, err
	}
	if r.Direction != "upload" || r.Job.HostBinding != cfg.Binding || cfg.sourcePath == "" {
		return ReceiptBinding{}, ErrBinding
	}
	j := r.Job
	task := deliverymeta.Task{OperationID: j.OperationID, BotID: j.BotID, UserID: j.Task.UserID, SessionID: j.Task.SessionID,
		TargetID: j.Task.TargetID, RunID: j.Task.RunID, Attempt: j.Task.Attempt, Generation: j.Task.Generation, RootHash: deliverymeta.RootHash(cfg.sourcePath)}
	if !task.Valid() {
		return ReceiptBinding{}, ErrBinding
	}
	if err := ctx.Err(); err != nil {
		return ReceiptBinding{}, err
	}
	out := ReceiptBinding{Schema: "memoh.delivery.receipt-binding.v1", State: "local_receipt_binding", Scope: "local_queue_read_only", ServerEffects: "none",
		DeliveryID: id, Task: task, HostBinding: j.HostBinding, ContentHash: j.Spec.SHA256, SizeBytes: j.Spec.SizeBytes, Mime: j.Spec.Mime,
		KnownReceipt: j.ReceiptID, Phase: j.Phase, Associated: r.Associated, AssociationState: r.AssociationState}
	if r.Source != nil {
		out.SourceRef = r.Source.Ref
	}
	return out, nil
}
