package computertask

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/toolcontext"
)

// resumeBatchWorkspace is an explicit continuation of the original request,
// never the ordinary duplicate-submit path or the read-only recovery API.
// Original task text must hash to the saved request; it is not reconstructed
// from native terminal content, a new conversation, or a model's summary.
func (s *Service) resumeBatchWorkspace(ctx context.Context, b toolcontext.ComputerBinding, id string, in Submit, target agenvo.Target) (Result, error) {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id || in.Kind != "herdr_pi_task" || target.NativeRuntimeID == "" || target.Agent.ExecutionMode != "pi-json-v1" {
		return Result{}, ErrInvalid
	}
	r, d, err := s.load(ctx, b, id)
	if err != nil {
		return Result{}, err
	}
	if r.TargetID != in.TargetID || r.TargetFingerprint != agenvo.TargetFingerprint(target) || r.Fingerprint != fingerprint(in) || d.Kind != in.Kind || d.Batch == nil || d.Batch.Progress.RequestHash != r.Fingerprint {
		return result(r, d), ErrConflict
	}
	if r.State != "unknown" {
		// A concurrent continuation or a known submission is an observation,
		// not permission to execute any of its stages again.
		return result(r, d), nil
	}
	if r.CancelRequested || d.Agent != (agenvo.AgentProgress{}) || d.Batch.Receipt != nil || d.Batch.Result != nil || len(d.Batch.Files) != 0 || d.Batch.Progress.RequestID != "" {
		return result(r, d), agenvo.ErrUnknown
	}
	switch d.Batch.Progress.Phase {
	case "batch_creating_workspace", "batch_workspace_adopted", "batch_tab_adopted":
	default:
		return result(r, d), agenvo.ErrUnknown
	}
	admitted, ok := s.store.(dbstore.ComputerOperationAdmissionStore)
	if !ok || s.authorize == nil {
		return result(r, d), ErrDenied
	}
	if err := agenvo.ValidateBatchRequest(d.Batch.Progress, target, in.WorkDir, in.Task); err != nil {
		return result(r, d), err
	}
	if err := s.authorize(ctx, b, r.TargetID, in.Kind, in.Task); err != nil {
		if errors.Is(err, ErrPreflightUnavailable) {
			return result(r, d), agenvo.ErrUnavailable
		}
		return result(r, d), err
	}
	pinned := agenvo.PinRegistration(ctx, scope(b, r.TargetID), r.TargetFingerprint)
	work, cancel := context.WithTimeout(pinned, time.Minute)
	defer cancel()

	// Any failed save can have committed. Stop this worker and re-read instead
	// of continuing from its speculative progress or overwriting a CAS winner.
	persistenceFailed := false
	reload := func(cause error) (Result, error) {
		readCtx, stop := context.WithTimeout(context.WithoutCancel(pinned), 3*time.Second)
		defer stop()
		current, details, readErr := s.load(readCtx, b, id)
		if readErr != nil {
			return Result{}, readErr
		}
		return result(current, details), cause
	}
	save := func(p agenvo.BatchProgress) error {
		preDispatch := p.Phase == "batch_creating_tab" || p.Phase == "batch_submitting"
		if preDispatch {
			// Persistence may outlive cancellation, but admission to a new
			// native input may not. Reuse the current complete target policy.
			if work.Err() != nil {
				persistenceFailed = true
				return agenvo.ErrUnknown
			}
			if err := s.authorize(work, b, r.TargetID, in.Kind, in.Task); err != nil {
				persistenceFailed = true
				if errors.Is(err, ErrPreflightUnavailable) {
					return agenvo.ErrUnavailable
				}
				return err
			}
		}
		persistCtx, stop := context.WithTimeout(context.WithoutCancel(pinned), 3*time.Second)
		defer stop()
		current, _, readErr := s.load(persistCtx, b, id)
		if readErr != nil || current.Revision != r.Revision || current.CancelRequested {
			persistenceFailed = true
			return agenvo.ErrUnknown
		}
		d.Batch.Progress, d.Phase = p, p.Phase
		next := r
		next.State = "unknown"
		if preDispatch {
			next.State = "dispatching"
		} else if p.Phase == "batch_submitted" {
			next.State = "submitted"
		}
		next.Details, readErr = resumedBatchDetails(r.Details, p)
		if readErr != nil {
			persistenceFailed = true
			return agenvo.ErrUnknown
		}
		var saved dbstore.ComputerOperationRecord
		var saveErr error
		if preDispatch {
			// The existing admission lock and revision CAS are one transaction.
			// Only its confirmed winner may issue the following native mutation.
			saved, saveErr = admitted.SaveComputerOperationWithAdmission(persistCtx, next)
		} else {
			saved, saveErr = s.store.SaveComputerOperation(persistCtx, next)
		}
		if saveErr != nil {
			persistenceFailed = true
			if admissionBlocked(saveErr) {
				return saveErr
			}
			return agenvo.ErrUnknown
		}
		r = saved
		if json.Unmarshal(r.Details, &d) != nil {
			persistenceFailed = true
			return agenvo.ErrUnknown
		}
		return nil
	}
	if d.Batch.Progress.Phase == "batch_creating_workspace" {
		p, adoptErr := s.relay.AdoptBatchWorkspace(work, scope(b, r.TargetID), in.WorkDir, d.Batch.Progress)
		if adoptErr != nil {
			return reload(adoptErr)
		}
		if err := save(p); err != nil {
			return reload(err)
		}
	}
	p, runErr := s.relay.ContinueBatch(work, scope(b, r.TargetID), in.Task, in.WorkDir, d.Batch.Progress, save)
	if persistenceFailed {
		return reload(runErr)
	}
	if runErr != nil {
		// Keep the uncertain pre-dispatch phase; a later explicit continuation
		// cannot enter creating_tab/submitting and therefore cannot replay it.
		d.Batch.Progress, d.Phase = p, p.Phase
		r.State = "unknown"
		r.Details, err = resumedBatchDetails(r.Details, p)
		if err != nil {
			return reload(agenvo.ErrUnknown)
		}
		persistCtx, stop := context.WithTimeout(context.WithoutCancel(pinned), 3*time.Second)
		defer stop()
		if _, saveErr := s.store.SaveComputerOperation(persistCtx, r); saveErr != nil {
			return reload(agenvo.ErrUnknown)
		}
		return reload(runErr)
	}
	return reload(nil)
}

// Change only the observed execution references and phase. Preserve unknown
// historical fields as well as the original request, producer and file ledger.
func resumedBatchDetails(raw dbstore.JSON, p agenvo.BatchProgress) (dbstore.JSON, error) {
	var details, batch, progress map[string]json.RawMessage
	if json.Unmarshal(raw, &details) != nil || json.Unmarshal(details["batch"], &batch) != nil || json.Unmarshal(batch["progress"], &progress) != nil || details == nil || batch == nil || progress == nil {
		return nil, agenvo.ErrUnknown
	}
	for key, value := range map[string]string{
		"phase": p.Phase, "workspace_id": p.WorkspaceID,
		"control_pane": p.ControlPane, "control_terminal": p.ControlTerminal,
		"pane_id": p.PaneID, "terminal_id": p.TerminalID, "request_id": p.RequestID,
	} {
		progress[key], _ = json.Marshal(value)
	}
	batch["progress"], _ = json.Marshal(progress)
	details["batch"], _ = json.Marshal(batch)
	details["phase"], _ = json.Marshal(p.Phase)
	return json.Marshal(details)
}
