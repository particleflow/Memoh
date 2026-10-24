package botdelegation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type deadlineHost struct{ *hostFixture }

func (h deadlineHost) Run(ctx context.Context, _ Record, _ func(string) error) (Result, string, error) {
	h.runs.Add(1)
	<-ctx.Done()
	// A late Host response must not turn an expired attempt into delivered success.
	return Result{Summary: &Summary{Text: "late", Source: "assistant_final"}}, "completed", nil
}

func TestDelegationDeadlineIsUnknownAndNeverReexecutes(t *testing.T) {
	s, _, h, in := serviceFixture(t)
	s.executionTimeout = 20 * time.Millisecond
	s.Host = deadlineHost{h}
	v, err := s.Submit(t.Context(), "manager", "user", "child", in)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	for {
		v, err = s.Get(t.Context(), "manager", "user", "child", v.ID)
		if err != nil {
			t.Fatal(err)
		}
		if v.State == "unknown" {
			break
		}
		select {
		case <-deadline:
			t.Fatal("deadline did not settle the operation")
		case <-time.After(time.Millisecond):
		}
	}
	if v.Summary != nil || len(v.Attachments) != 0 || h.deliveries.Load() != 0 {
		t.Fatal("expired result delivered")
	}
	if _, err := s.RetryDelivery(t.Context(), "manager", "user", "child", v.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("unknown delivery retried: %v", err)
	}
	replay, err := s.Submit(t.Context(), "manager", "user", "child", in)
	if err != nil || replay.ID != v.ID || h.runs.Load() != 1 {
		t.Fatalf("business reexecuted: %v, calls %d", err, h.runs.Load())
	}
}

func TestDelegationExecutionContextConfig(t *testing.T) {
	for _, duration := range []time.Duration{0, 2 * time.Hour} {
		s, _, _, _ := serviceFixture(t)
		s.executionTimeout = duration
		parent, stop := context.WithCancel(t.Context())
		ctx, cancel := s.executionContext(parent)
		when, ok := ctx.Deadline()
		if duration == 0 && ok {
			t.Fatal("disabled outer deadline still present")
		}
		if duration > 0 && (!ok || time.Until(when) < duration-time.Second || time.Until(when) > duration) {
			t.Fatal("configured long deadline was not applied")
		}
		stop()
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatal("shutdown cancellation lost")
		}
		cancel()
	}
}

func TestDelegationPersistsHostRouteNotInputRoute(t *testing.T) {
	s, _, h, in := serviceFixture(t)
	h.block = make(chan struct{})
	defer close(h.block)
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields["return_route"] = map[string]any{"channel": "telegram", "route_id": "forged", "reply_target": "other-user"}
	fields["reply_target"] = "other-user"
	raw, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		t.Fatal(err)
	}
	v, err := s.Submit(t.Context(), "manager", "user", "child", in)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.ReadRecord(t.Context(), "manager", "user", "child", v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Payload.ReturnRoute == nil || r.Payload.ReturnRoute.Channel != "web" || r.Payload.ReturnRoute.ReplyTarget != "manager" || r.Payload.ReturnRoute.RouteID != "" {
		t.Fatal("caller chose the delivery route")
	}
	encoded, err := json.Marshal(r.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var restored Payload
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.ReturnRoute == nil || *restored.ReturnRoute != *r.Payload.ReturnRoute {
		t.Fatal("route lost across durable payload roundtrip")
	}
}
