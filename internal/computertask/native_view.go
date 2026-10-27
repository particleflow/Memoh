package computertask

import (
	"context"
	"time"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/toolcontext"
)

// NativeContent is a bounded terminal sample, not complete logs or verified
// per-input output. Reads do not append to the durable evidence ledger.
type NativeContent struct {
	OperationID       string                   `json:"operation_id"`
	Supported         bool                     `json:"supported"`
	Availability      string                   `json:"availability"`
	FollowupAvailable bool                     `json:"followup_available"`
	SessionBinding    string                   `json:"session_binding"`
	Observation       *agenvo.AgentObservation `json:"observation,omitempty"`
	Followups         []FollowupResult         `json:"followups,omitempty"`
}

func (s *Source) NativeContent(ctx context.Context, b toolcontext.ComputerBinding, id string) (NativeContent, error) {
	service := s.service
	r, d, err := service.load(ctx, b, id)
	if err != nil {
		return NativeContent{}, err
	}
	out := NativeContent{OperationID: id, Availability: "unsupported", SessionBinding: "unavailable"}
	if d.Kind != "herdr_pi_task" || d.Batch != nil || d.Agent.Name == "" {
		return out, nil
	}
	out.Supported = true
	saved := result(r, d)
	out.SessionBinding, out.Followups = saved.SessionBinding, saved.Followups
	pinned := agenvo.PinRegistration(ctx, scope(b, r.TargetID), r.TargetFingerprint)
	read, cancel := context.WithTimeout(pinned, 10*time.Second)
	defer cancel()
	obs, readErr := service.relay.ObserveAgent(read, scope(b, r.TargetID), d.Agent)
	if observationDenied(readErr) {
		return NativeContent{}, ErrDenied
	}
	r, current, err := service.load(pinned, b, id)
	if err != nil {
		return NativeContent{}, err
	}
	if fingerprint(current.Agent) != fingerprint(d.Agent) {
		return NativeContent{}, agenvo.ErrTargetChanged
	}
	out.Followups = result(r, current).Followups
	out.Availability = agenvo.Availability(readErr)
	if readErr == nil {
		out.Observation = &obs
	}
	return out, nil
}

// FollowupNative uses the operation's persisted run attribution; an HTTP caller
// cannot invent a run or native reference. Only persisted input receipts replay.
func (s *Source) FollowupNative(ctx context.Context, b toolcontext.ComputerBinding, in Followup) (Result, error) {
	r, _, err := s.service.load(ctx, b, in.OperationID)
	if err != nil {
		return Result{}, err
	}
	if r.RunID == "" {
		return Result{}, agenvo.ErrUnsupported
	}
	b.RunID = r.RunID
	return s.service.followup(ctx, b, in)
}
