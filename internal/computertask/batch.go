package computertask

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/toolcontext"
)

// A batch is an ordered durable queue of herdr_pi_task submissions that the
// existing result-delivery scan advances one item at a time. There is no new
// scheduler, no new concurrency rule and no new endpoint protocol: each item is
// dispatched through the unchanged single-task path, so it inherits that path's
// unsettled-slot exclusivity, target pinning, admission trigger and delivery
// ledger.
//
// Item lifecycle: queued -> in_flight(operation) -> succeeded | failed.
// Cancellation moves only queued items to canceled. An in_flight item whose
// operation outcome stays unknown keeps the existing unsettled slot: it records
// Blocked and the batch reports blocked until that operation settles. Nothing
// is retried, skipped or bypassed on the batch's behalf.
const (
	BatchItemQueued    = "queued"
	BatchItemInFlight  = "in_flight"
	BatchItemSucceeded = "succeeded"
	BatchItemFailed    = "failed"
	BatchItemCanceled  = "canceled"

	BatchRunning   = "running"
	BatchBlocked   = "blocked"
	BatchCompleted = "completed"
)

// ErrBatchUnavailable names a missing optional batch store. Every single-task
// path stays available when it is returned.
var ErrBatchUnavailable = errors.New("computer task batches are unavailable in this conversation")

// BatchItemSpec carries the same request fields the single-task submit accepts,
// minus resume (a batch never resumes an earlier operation) and minus the
// target (the batch pins one target for every item).
type BatchItemSpec struct {
	Kind      string                  `json:"kind"`
	Task      string                  `json:"task"`
	Model     string                  `json:"model"`
	Inputs    []string                `json:"inputs,omitempty"`
	Artifact  *deliverymeta.Artifact  `json:"artifact,omitempty"`
	Artifacts []deliverymeta.Artifact `json:"artifacts,omitempty"`
	WorkDir   string                  `json:"work_dir,omitempty"`
}

// Submit converts the stored spec into the canonical single-task request. The
// batch's pinned target travels explicitly, so the claim fingerprint of a
// batched item equals a direct computer_task_submit of the same body. batchID
// marks the operation as batch-owned without entering that fingerprint.
func (i BatchItemSpec) Submit(target, batchID string) Submit {
	return Submit{TargetID: target, Kind: i.Kind, Task: i.Task, Model: i.Model, Inputs: i.Inputs, Artifact: i.Artifact, Artifacts: i.Artifacts, WorkDir: i.WorkDir, BatchOwner: batchID}
}

// BatchItem is one durable queue entry. OperationID is the single-task receipt
// this item claimed; Outcome and Error are set only at a terminal transition.
type BatchItem struct {
	Spec        BatchItemSpec `json:"spec"`
	State       string        `json:"state"`
	OperationID string        `json:"operation_id,omitempty"`
	Outcome     string        `json:"outcome,omitempty"`
	Error       string        `json:"error,omitempty"`
	Blocked     bool          `json:"blocked,omitempty"`
}

type BatchRequest struct {
	Label    string          `json:"label,omitempty"`
	TargetID string          `json:"target_id"`
	Items    []BatchItemSpec `json:"items"`
}

// BatchSummary is the bounded caller view: complete counts plus the advancing
// head and the non-succeeded items (capped, with MoreItems counting the rest).
type BatchSummary struct {
	ID        string          `json:"batch_id"`
	Label     string          `json:"label,omitempty"`
	State     string          `json:"state"`
	TargetID  string          `json:"target_id"`
	Total     int             `json:"total"`
	Succeeded int             `json:"succeeded"`
	Failed    int             `json:"failed"`
	Canceled  int             `json:"canceled"`
	Queued    int             `json:"queued"`
	InFlight  int             `json:"in_flight"`
	BlockedOn string          `json:"blocked_on,omitempty"`
	Head      *BatchItemLine  `json:"head,omitempty"`
	Items     []BatchItemLine `json:"items,omitempty"`
	MoreItems int             `json:"more_items,omitempty"`
	Created   bool            `json:"created,omitempty"`
	CreatedAt time.Time       `json:"created_at,omitzero"`
	UpdatedAt time.Time       `json:"updated_at,omitzero"`
}

type BatchItemLine struct {
	Position    int    `json:"position"`
	State       string `json:"state"`
	OperationID string `json:"operation_id,omitempty"`
	Outcome     string `json:"outcome,omitempty"`
	Error       string `json:"error,omitempty"`
	Task        string `json:"task,omitempty"`
}

// batchSummaryLimit bounds how many non-succeeded item lines one read returns.
// Counts stay complete regardless; the cap only bounds the response body.
const batchSummaryLimit = 20

// batchTaskLimit bounds the per-item task text shown in a summary line.
const batchTaskLimit = 120

// WithComputerBatches injects the ordered queue store and the host callback that
// rebuilds one batch's persisted conversation authority. Without both, the batch
// tools return ErrBatchUnavailable and every single-task path is untouched.
func (s *Service) WithComputerBatches(queue dbstore.ComputerTaskBatchQueue, rebuild func(context.Context, toolcontext.ComputerBinding) (context.Context, error)) *Service {
	s.batches, s.batchContext = queue, rebuild
	return s
}

func validBatchItemSpec(item BatchItemSpec) error {
	if item.Kind != "herdr_pi_task" || strings.TrimSpace(item.Task) == "" || len(item.Task) > 32000 || !utf8.ValidString(item.Task) || strings.ContainsRune(item.Task, 0) || strings.TrimSpace(item.Model) == "" || len(item.Inputs) > deliverymeta.MaxArtifacts {
		return ErrInvalid
	}
	seen := make(map[string]bool, len(item.Inputs))
	for _, hash := range item.Inputs {
		if !deliveryHash.MatchString(hash) || seen[hash] {
			return ErrInvalid
		}
		seen[hash] = true
	}
	workDir := item.WorkDir
	if workDir == "" {
		workDir = "."
	}
	if (item.Artifact != nil || item.Artifacts != nil) && (item.Artifact != nil && (item.Artifacts != nil || !item.Artifact.Valid()) || item.Artifacts != nil && !deliverymeta.ValidArtifacts(item.Artifacts) || workDir != ".") {
		return ErrInvalid
	}
	return nil
}

// BatchSubmit validates and durably enqueues one ordered batch. Static
// validation, target authorization and the live model catalog run here for the
// whole batch; each item still revalidates through the unchanged single-task
// submit at its own dispatch time. A replay of the same request from the same
// run returns the original batch and creates no work.
func (s *Service) BatchSubmit(ctx context.Context, b toolcontext.ComputerBinding, in BatchRequest) (BatchSummary, error) {
	if s.batches == nil {
		return BatchSummary{}, ErrBatchUnavailable
	}
	if b.RunID == "" || strings.TrimSpace(in.TargetID) == "" || len(in.Items) == 0 || len(in.Label) > 200 || !utf8.ValidString(in.Label) {
		return BatchSummary{}, ErrInvalid
	}
	for _, item := range in.Items {
		if err := validBatchItemSpec(item); err != nil {
			return BatchSummary{}, err
		}
	}
	if _, err := s.registry.GetMount(ctx, b.BotID, in.TargetID); err != nil {
		return BatchSummary{}, ErrDenied
	}
	if s.authorize == nil {
		return BatchSummary{}, ErrDenied
	}
	if err := s.authorize(ctx, b, in.TargetID, "herdr_pi_task", in.Items[0].Task); err != nil {
		return BatchSummary{}, err
	}
	// Validate every declared model against the live catalog before anything is
	// durable, so a typo cannot produce a queue of individually failing items.
	for _, item := range in.Items {
		if _, err := s.requirePiModel(ctx, item.Submit(in.TargetID, "")); err != nil {
			return BatchSummary{}, err
		}
	}
	items := make([]BatchItem, 0, len(in.Items))
	for _, spec := range in.Items {
		items = append(items, BatchItem{Spec: spec, State: BatchItemQueued})
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return BatchSummary{}, ErrInvalid
	}
	body := struct {
		Label    string          `json:"label"`
		TargetID string          `json:"target_id"`
		Items    []BatchItemSpec `json:"items"`
	}{Label: strings.TrimSpace(in.Label), TargetID: in.TargetID, Items: in.Items}
	rec := dbstore.ComputerTaskBatchRecord{ID: uuid.NewString(), BotID: b.BotID, SessionID: b.SessionID, UserID: b.UserID, TargetID: in.TargetID, RunID: b.RunID, Label: body.Label, Fingerprint: fingerprint(body), State: BatchRunning, Items: raw}
	rec, created, err := s.batches.ClaimComputerTaskBatch(ctx, rec)
	if err != nil {
		return BatchSummary{}, ErrDenied
	}
	out := BatchSummaryFromRecord(rec)
	out.Created = created
	return out, nil
}

// BatchStatus reads one batch by id, or the session's most recent batches when
// id is empty. Read-only: no dispatch, no observation, no wake.
func (s *Service) BatchStatus(ctx context.Context, b toolcontext.ComputerBinding, id string) ([]BatchSummary, error) {
	if s.batches == nil {
		return nil, ErrBatchUnavailable
	}
	if id = strings.TrimSpace(id); id != "" {
		rec, err := s.batches.ReadComputerTaskBatch(ctx, id, b.BotID, b.SessionID, b.UserID)
		if err != nil {
			return nil, ErrDenied
		}
		return []BatchSummary{BatchSummaryFromRecord(rec)}, nil
	}
	recs, err := s.batches.ListSessionComputerTaskBatches(ctx, b.BotID, b.SessionID, b.UserID, 20)
	if err != nil {
		return nil, ErrDenied
	}
	out := make([]BatchSummary, 0, len(recs))
	for _, rec := range recs {
		out = append(out, BatchSummaryFromRecord(rec))
	}
	return out, nil
}

// BatchCancel cancels every not-yet-dispatched item. The in-flight item keeps
// the existing operation semantics (computer_task_cancel); a batch left with no
// advancing item completes normally.
func (s *Service) BatchCancel(ctx context.Context, b toolcontext.ComputerBinding, id string) (BatchSummary, error) {
	if s.batches == nil {
		return BatchSummary{}, ErrBatchUnavailable
	}
	if id = strings.TrimSpace(id); id == "" {
		return BatchSummary{}, ErrInvalid
	}
	if _, err := s.batches.ReadComputerTaskBatch(ctx, id, b.BotID, b.SessionID, b.UserID); err != nil {
		return BatchSummary{}, ErrDenied
	}
	if err := s.batches.ChangeComputerTaskBatch(ctx, id, func(rec *dbstore.ComputerTaskBatchRecord) error {
		items, err := DecodeBatchItems(rec.Items)
		if err != nil {
			return err
		}
		for i := range items {
			if items[i].State == BatchItemQueued {
				items[i].State = BatchItemCanceled
			}
		}
		return storeBatchItems(rec, items)
	}); err != nil {
		return BatchSummary{}, ErrDenied
	}
	rec, err := s.batches.ReadComputerTaskBatch(ctx, id, b.BotID, b.SessionID, b.UserID)
	if err != nil {
		return BatchSummary{}, ErrDenied
	}
	return BatchSummaryFromRecord(rec), nil
}

// DecodeBatchItems parses a stored item array. A malformed array is an error,
// never an empty batch: hiding items could mint duplicate dispatches.
func DecodeBatchItems(raw []byte) ([]BatchItem, error) {
	var items []BatchItem
	if len(raw) == 0 || json.Unmarshal(raw, &items) != nil || len(items) == 0 {
		return nil, ErrInvalid
	}
	for _, item := range items {
		if !validBatchState(item.State) || validBatchItemSpec(item.Spec) != nil {
			return nil, ErrInvalid
		}
	}
	return items, nil
}

func validBatchState(state string) bool {
	switch state {
	case BatchItemQueued, BatchItemInFlight, BatchItemSucceeded, BatchItemFailed, BatchItemCanceled:
		return true
	}
	return false
}

func storeBatchItems(rec *dbstore.ComputerTaskBatchRecord, items []BatchItem) error {
	raw, err := json.Marshal(items)
	if err != nil {
		return ErrInvalid
	}
	rec.Items, rec.State = raw, BatchItemsState(items)
	return nil
}

// BatchItemsState derives the batch state from its items: blocked while the
// in-flight item holds an unresolved operation slot, running while any item can
// still advance, completed once no item can.
func BatchItemsState(items []BatchItem) string {
	for _, item := range items {
		switch item.State {
		case BatchItemQueued:
			return BatchRunning
		case BatchItemInFlight:
			if item.Blocked {
				return BatchBlocked
			}
			return BatchRunning
		}
	}
	return BatchCompleted
}

// BatchSummaryFromRecord projects the durable row into the bounded caller view.
func BatchSummaryFromRecord(rec dbstore.ComputerTaskBatchRecord) BatchSummary {
	out := BatchSummary{ID: rec.ID, Label: rec.Label, State: rec.State, TargetID: rec.TargetID, CreatedAt: rec.CreatedAt, UpdatedAt: rec.UpdatedAt}
	items, err := DecodeBatchItems(rec.Items)
	if err != nil {
		out.State, out.Total = "unavailable", -1
		return out
	}
	out.Total = len(items)
	for i, item := range items {
		switch item.State {
		case BatchItemSucceeded:
			out.Succeeded++
		case BatchItemFailed:
			out.Failed++
		case BatchItemCanceled:
			out.Canceled++
		case BatchItemQueued:
			out.Queued++
			if out.Head == nil {
				line := batchItemLine(i, item)
				out.Head = &line
			}
		case BatchItemInFlight:
			out.InFlight++
			if out.Head == nil {
				line := batchItemLine(i, item)
				out.Head = &line
				if item.Blocked {
					out.BlockedOn = item.OperationID
				}
			}
		}
	}
	for i, item := range items {
		if item.State == BatchItemSucceeded {
			continue
		}
		if len(out.Items) < batchSummaryLimit {
			out.Items = append(out.Items, batchItemLine(i, item))
			continue
		}
		out.MoreItems++
	}
	return out
}

func batchItemLine(position int, item BatchItem) BatchItemLine {
	task := item.Spec.Task
	if runes := []rune(task); len(runes) > batchTaskLimit {
		task = string(runes[:batchTaskLimit])
	}
	return BatchItemLine{Position: position, State: item.State, OperationID: item.OperationID, Outcome: item.Outcome, Error: item.Error, Task: task}
}
