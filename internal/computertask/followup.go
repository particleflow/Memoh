package computertask

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/toolcontext"
)

const maxFollowups = 64

type Followup struct {
	OperationID string `json:"operation_id"`
	RequestID   string `json:"request_id"`
	Mode        string `json:"mode"`
	Task        string `json:"task"`
}

// FollowupRecord is appended to the original operation's JSON under its
// revision CAS. Fingerprints are local deduplication, not a Herdr generation.
// Prompt text and private session paths are never projected in the response.
type FollowupRecord struct {
	RequestID   string                       `json:"request_id"`
	Fingerprint string                       `json:"fingerprint"`
	RunID       string                       `json:"run_id"`
	Mode        string                       `json:"mode"`
	Progress    agenvo.AgentFollowupProgress `json:"progress"`
}

type FollowupResult struct {
	RequestID                string `json:"request_id"`
	Mode                     string `json:"mode"`
	State                    string `json:"state"`
	Reason                   string `json:"reason"`
	SessionRelation          string `json:"session_relation"`
	SessionObserved          bool   `json:"session_observed"`
	InputMayHaveBeenSent     bool   `json:"input_may_have_been_sent"`
	PromptAccepted           bool   `json:"prompt_accepted"`
	BusinessOutcome          string `json:"business_outcome"`
	ContinuityVerified       bool   `json:"continuity_verified"`
	AtomicIdentityGuaranteed bool   `json:"atomic_identity_guaranteed"`
	StrictResumeAvailable    bool   `json:"strict_resume_available"`
}

func followupResult(f FollowupRecord) FollowupResult {
	p := f.Progress
	out := FollowupResult{RequestID: f.RequestID, Mode: f.Mode, State: p.State, Reason: p.Reason, SessionRelation: p.SessionRelation, SessionObserved: p.Session.Valid(), BusinessOutcome: "unknown", PromptAccepted: p.State == "accepted", InputMayHaveBeenSent: p.State == "dispatching" || p.State == "unknown" || p.State == "accepted"}
	if p.State == "reserved" || p.State == "dispatching" {
		out.State, out.Reason = "unknown", "request_in_progress_or_interrupted"
	}
	return out
}

func followupResponse(r dbstore.ComputerOperationRecord, d Details, id string) Result {
	out := result(r, d)
	for _, f := range d.Followups {
		if f.RequestID == id {
			v := followupResult(f)
			out.Followup = &v
		}
	}
	return out
}

func followupError(f FollowupRecord) error {
	switch f.Progress.State {
	case "accepted":
		return nil
	case "rejected":
		return agenvo.ErrRejected
	default:
		return agenvo.ErrUnknown
	}
}

// Patch only the new ledger key, preserving all existing details, including
// delivery associations and fields owned by other application components.
func withFollowups(raw dbstore.JSON, records []FollowupRecord) (dbstore.JSON, error) {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return nil, agenvo.ErrUnknown
	}
	value, err := json.Marshal(records)
	if err != nil {
		return nil, agenvo.ErrUnknown
	}
	object["followups"] = value
	return json.Marshal(object)
}

func (s *Service) saveFollowups(ctx context.Context, r dbstore.ComputerOperationRecord, records []FollowupRecord) (dbstore.ComputerOperationRecord, error) {
	raw, err := withFollowups(r.Details, records)
	if err != nil {
		return r, err
	}
	r.Details = raw
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	return s.store.SaveComputerOperation(persist, r)
}

func (s *Service) followup(ctx context.Context, b toolcontext.ComputerBinding, in Followup) (Result, error) {
	id, err := uuid.Parse(in.RequestID)
	if err != nil || id == uuid.Nil || id.String() != in.RequestID || in.OperationID == "" || b.RunID == "" || (in.Mode != "current_agent" && in.Mode != "bound_session") || strings.TrimSpace(in.Task) == "" || len(in.Task) > 32000 || !utf8.ValidString(in.Task) || strings.ContainsRune(in.Task, 0) {
		return Result{}, ErrInvalid
	}
	r, d, err := s.load(ctx, b, in.OperationID)
	if err != nil {
		return Result{}, err
	}
	requestHash := fingerprint(in)
	for _, old := range d.Followups {
		if old.RequestID == in.RequestID {
			out := followupResponse(r, d, in.RequestID)
			if old.Fingerprint != requestHash {
				out.RequestConflict = true
				return out, ErrInvalid
			}
			return out, followupError(old)
		}
	}
	if d.Kind != "herdr_pi_task" || d.Batch != nil || !d.Agent.PromptSubmitted || d.Agent.Phase != "prompt_submitted" || r.CancelRequested {
		return result(r, d), agenvo.ErrUnsupported
	}
	for _, old := range d.Followups {
		if old.Progress.State != "accepted" && old.Progress.State != "rejected" {
			out := followupResponse(r, d, old.RequestID)
			out.BlockedByRequest, out.BlockedByOperation = old.RequestID, r.ID
			return out, ErrConflict
		}
	}
	if len(d.Followups) >= maxFollowups {
		return result(r, d), ErrInvalid
	}
	if err := s.authorizeFollowup(ctx, b, r.TargetID, in.Task); err != nil {
		return result(r, d), err
	}
	f := FollowupRecord{RequestID: in.RequestID, Fingerprint: requestHash, RunID: b.RunID, Mode: in.Mode, Progress: agenvo.AgentFollowupProgress{State: "reserved", Reason: "request_reserved", SessionRelation: "unbound"}}
	d.Followups = append(d.Followups, f)
	claimed, err := s.saveFollowups(ctx, r, d.Followups)
	if err != nil {
		if admissionBlocked(err) {
			// The database rejected this reservation before accepting input.
			// Project the persisted receipt, never the unsaved appended record.
			var previous Details
			if json.Unmarshal(r.Details, &previous) != nil {
				return Result{}, agenvo.ErrUnknown
			}
			return result(r, previous), err
		}
		// A conflicting/lost CAS response never authorizes a prompt. Reload
		// the actual ledger; even our own committed reservation is not resumed.
		current, details, readErr := s.load(ctx, b, r.ID)
		if readErr != nil {
			return Result{}, readErr
		}
		out := followupResponse(current, details, in.RequestID)
		for _, old := range details.Followups {
			if old.RequestID == in.RequestID && old.Fingerprint != requestHash {
				// Apply the same intent check as initial deduplication: the
				// CAS winner's receipt does not accept different arguments.
				out.RequestConflict = true
				return out, ErrInvalid
			}
		}
		return out, agenvo.ErrUnknown
	}
	r = claimed
	work, cancel := context.WithTimeout(agenvo.PinRegistration(ctx, scope(b, r.TargetID), r.TargetFingerprint), 35*time.Second)
	defer cancel()
	p, _ := s.relay.PromptAgent(work, scope(b, r.TargetID), d.Agent, in.Mode, in.Task, func(p agenvo.AgentFollowupProgress) error {
		current, details, err := s.load(work, b, r.ID)
		if err != nil || current.CancelRequested || work.Err() != nil {
			return ErrDenied
		}
		if err := s.authorizeFollowup(work, b, current.TargetID, in.Task); err != nil {
			return err
		}
		for i, pending := range details.Followups {
			if pending.RequestID == f.RequestID && pending.Fingerprint == f.Fingerprint && pending.Progress.State == "reserved" {
				details.Followups[i].Progress = p
				_, err = s.saveFollowups(work, current, details.Followups)
				return err
			}
		}
		return agenvo.ErrUnknown
	})
	f.Progress = p
	// Finishing changes only this request. Concurrent observation/cancellation
	// must not erase its receipt or have their own details overwritten.
	finishErr := s.finishFollowup(ctx, r, f)
	current, details, err := s.load(ctx, b, r.ID)
	if err != nil {
		return Result{}, err // Never release receipts after scope/grant changes.
	}
	out := followupResponse(current, details, in.RequestID)
	if finishErr != nil {
		return out, agenvo.ErrUnknown
	}
	for _, saved := range details.Followups {
		if saved.RequestID == in.RequestID {
			return out, followupError(saved)
		}
	}
	return out, agenvo.ErrUnknown
}

func (s *Service) authorizeFollowup(ctx context.Context, b toolcontext.ComputerBinding, target, task string) error {
	if s.authorize == nil || ctx.Err() != nil {
		return ErrDenied
	}
	err := s.authorize(ctx, b, target, "herdr_pi_task", task)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrApproval):
		return ErrApproval
	case errors.Is(err, ErrPreflightUnavailable):
		return ErrPreflightUnavailable
	default:
		return ErrDenied // Private authorizer causes never become tool output.
	}
}

func (s *Service) finishFollowup(ctx context.Context, owner dbstore.ComputerOperationRecord, f FollowupRecord) error {
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	for range 3 {
		r, err := s.store.ReadComputerOperation(persist, owner)
		if err != nil {
			return agenvo.ErrUnknown
		}
		var d Details
		if json.Unmarshal(r.Details, &d) != nil {
			return agenvo.ErrUnknown
		}
		found := false
		for i, old := range d.Followups {
			if old.RequestID != f.RequestID || old.Fingerprint != f.Fingerprint {
				continue
			}
			if old.Progress.State == "accepted" || old.Progress.State == "rejected" || old.Progress.State == "unknown" {
				return nil // Already committed; never rewrite a settled receipt.
			}
			d.Followups[i], found = f, true
		}
		if !found {
			return agenvo.ErrUnknown
		}
		if _, err := s.saveFollowups(persist, r, d.Followups); err == nil {
			return nil
		}
	}
	return agenvo.ErrUnknown
}
