package computertask

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/toolcontext"
)

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

// Only existing historical input receipts are exposed; no new input is written.

func (s *Service) followup(ctx context.Context, b toolcontext.ComputerBinding, in Followup) (Result, error) {
	id, err := uuid.Parse(in.RequestID)
	if err != nil || id == uuid.Nil || id.String() != in.RequestID || in.OperationID == "" || b.RunID == "" || (in.Mode != "current_agent" && in.Mode != "bound_session") || strings.TrimSpace(in.Task) == "" || len(in.Task) > 32000 || !utf8.ValidString(in.Task) || strings.ContainsRune(in.Task, 0) {
		return Result{}, ErrInvalid
	}
	r, d, err := s.loadHistory(ctx, b, in.OperationID)
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
	// Existing input receipts remain queryable. Retired TUI input is never
	// reserved, sent or retried, including for a previously unknown operation.
	return result(r, d), agenvo.ErrUnsupported
}
