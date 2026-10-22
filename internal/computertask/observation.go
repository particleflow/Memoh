package computertask

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/toolcontext"
)

type WorkspaceDiscovery struct {
	TargetID string `json:"target_id"`
	agenvo.WorkspaceObservations
}

type RecoverySummary struct {
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
	Source    string `json:"source"`
}

// Recovery is a read model. It has no transfer, submission or store-write port.
// Native presence is separate from durable process/Pi/business result facts.
type Recovery struct {
	OperationID     string                      `json:"operation_id"`
	TargetID        string                      `json:"target_id"`
	State           string                      `json:"state"`
	ProcessState    string                      `json:"process_state"`
	PiResult        string                      `json:"pi_result"`
	BusinessOutcome string                      `json:"business_outcome"`
	ResultReason    string                      `json:"result_reason"`
	CancelRequested bool                        `json:"cancel_requested"`
	Attempt         string                      `json:"attempt,omitempty"`
	PiSessionID     string                      `json:"pi_session_id,omitempty"`
	WorkspaceID     string                      `json:"workspace_id,omitempty"`
	PaneID          string                      `json:"pane_id,omitempty"`
	ResultSummary   *RecoverySummary            `json:"result_summary,omitempty"`
	Native          agenvo.ReferenceObservation `json:"native"`
}

func observationRef(value string) string {
	if len(value) > 256 || !utf8.ValidString(value) || strings.ContainsAny(value, "\x00\r\n") {
		return ""
	}
	return value
}

func observationHexRef(value string, length int) string {
	if len(value) != length || value != strings.ToLower(value) {
		return ""
	}
	if _, err := hex.DecodeString(value); err != nil {
		return ""
	}
	return value
}

func observationDenied(err error) bool {
	return errors.Is(err, agenvo.ErrForbidden) || errors.Is(err, agenvo.ErrTargetChanged) || errors.Is(err, toolcontext.ErrComputerScope) || errors.Is(err, toolcontext.ErrComputerBinding)
}

func (s *Service) workspaces(ctx context.Context, b toolcontext.ComputerBinding, requested string) (WorkspaceDiscovery, error) {
	if b.TargetID != "native" && requested != "" && requested != b.TargetID {
		return WorkspaceDiscovery{}, ErrDenied
	}
	id, target, err := s.choose(ctx, b, requested, "foreground_bash")
	if err != nil {
		return WorkspaceDiscovery{}, err
	}
	pinned := agenvo.PinRegistration(ctx, scope(b, id), agenvo.TargetFingerprint(target))
	readCtx, cancel := context.WithTimeout(pinned, 10*time.Second)
	defer cancel()
	observation, err := s.relay.Workspaces(readCtx, scope(b, id))
	if observationDenied(err) {
		return WorkspaceDiscovery{}, ErrDenied
	}
	if _, currentErr := s.relay.ResolveTarget(pinned, scope(b, id)); currentErr != nil {
		return WorkspaceDiscovery{}, ErrDenied
	}
	if err != nil {
		observation.Workspaces = []agenvo.WorkspaceObservation{}
		observation.Truncated = false
		observation.Availability = agenvo.Availability(err)
	}
	return WorkspaceDiscovery{TargetID: id, WorkspaceObservations: observation}, nil
}

func (s *Service) recover(ctx context.Context, b toolcontext.ComputerBinding, id string) (Recovery, error) {
	r, d, err := s.load(ctx, b, id)
	if err != nil {
		return Recovery{}, err
	}
	if b.TargetID != "native" && b.TargetID != r.TargetID {
		return Recovery{}, ErrDenied
	}
	state := r.State
	if state == "dispatching" {
		state = "unknown"
	}
	out := Recovery{OperationID: r.ID, TargetID: r.TargetID, State: state, ProcessState: "unknown", PiResult: "unknown", BusinessOutcome: "unknown", ResultReason: "receipt_missing", CancelRequested: r.CancelRequested}
	refs := agenvo.OperationReferences{Workspace: d.Agent.WorkspaceID, Pane: d.Agent.PaneID, Terminal: d.Agent.TerminalID}
	if batch := d.Batch; batch != nil {
		p := batch.Progress
		refs = agenvo.OperationReferences{Workspace: p.WorkspaceID, Pane: p.PaneID, Terminal: p.TerminalID, ControlPane: p.ControlPane, ControlTerminal: p.ControlTerminal}
		out.Attempt, out.PiSessionID = observationHexRef(p.Attempt, 32), observationHexRef(p.PiSessionID, 64)
		if p.CompletionVersion != agenvo.BatchCompletionVersion {
			out.ResultReason = "legacy_result_protocol"
		}
		if batch.Receipt != nil {
			if batch.Receipt.Validate(p) != nil {
				out.ResultReason = "receipt_mismatch"
			} else {
				out.ProcessState = "exited"
				switch {
				case !batch.Receipt.Started:
					out.ProcessState, out.PiResult, out.ResultReason = "not_started", "not_started", "verified_receipt"
				case batch.Result != nil && agenvo.ValidateBatchCompletionResult(*batch.Result, p, *batch.Receipt) == nil:
					out.PiResult, out.ResultReason = batch.Result.Outcome, "verified_persisted_result"
					if batch.Result.Output != "" {
						out.ResultSummary = &RecoverySummary{Text: batch.Result.Output, Truncated: batch.Result.Truncated, Source: "pi_final_assistant"}
					}
				case p.CompletionVersion == agenvo.BatchCompletionVersion:
					out.ResultReason = "completion_result_unavailable"
				}
			}
		}
	} else {
		out.ResultReason = "legacy_observation_only"
	}
	out.WorkspaceID, out.PaneID = observationRef(refs.Workspace), observationRef(refs.Pane)
	pinned := agenvo.PinRegistration(ctx, scope(b, r.TargetID), r.TargetFingerprint)
	readCtx, cancel := context.WithTimeout(pinned, 10*time.Second)
	defer cancel()
	out.Native, err = s.relay.ObserveReferences(readCtx, scope(b, r.TargetID), refs)
	if observationDenied(err) {
		return Recovery{}, ErrDenied
	}
	// Recheck owned session, current grant and registration before releasing
	// either persisted text or native metadata. No receipt is changed or minted.
	if _, _, err := s.load(pinned, b, r.ID); err != nil {
		return Recovery{}, err
	}
	return out, nil
}
