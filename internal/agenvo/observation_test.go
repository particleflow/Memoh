package agenvo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func TestWorkspaceObservationBoundedProjection(t *testing.T) {
	for _, scenario := range []string{"large", "duplicate", "null", "private_status", "wrong_session"} {
		t.Run(scenario, func(t *testing.T) {
			s, _, f := serviceFixture(t)
			s.call = func(_ context.Context, _, _, _ string, _ map[string]any) (json.RawMessage, error) {
				items := make([]map[string]any, 101)
				for i := range items {
					items[i] = map[string]any{"workspace_id": fmt.Sprintf("w-%d", i), "label": "Synthetic", "agent_status": "PRIVATE", "cwd": "SECRET", "pane_count": 1}
				}
				if scenario == "duplicate" {
					items[100]["workspace_id"] = "w-0"
				}
				if scenario == "null" {
					items = nil
				}
				session := f.target.Session
				if scenario == "wrong_session" {
					session = "foreign"
				}
				return mcpValue(map[string]any{"execution": "accepted", "result": map[string]any{"session": session, "result": map[string]any{"workspaces": items}}}), nil
			}
			got, err := s.Workspaces(t.Context(), request().Scope)
			if scenario == "duplicate" || scenario == "null" || scenario == "wrong_session" {
				if err == nil {
					t.Fatal("unverified list accepted")
				}
				return
			}
			if err != nil || !got.Truncated || len(got.Workspaces) != 100 || got.Workspaces[0].AgentStatus != "unknown" {
				t.Fatalf("bounded projection=%+v err=%v", got, err)
			}
		})
	}
}

func TestProbeAvailabilityRequiresTypedEvidence(t *testing.T) {
	for _, scenario := range []struct {
		name, execution, code, want string
		err                         error
	}{
		{name: "auth", want: "authentication_unavailable", err: errors.Join(ErrAuthenticationUnavailable, errors.New("SECRET"))},
		{name: "text_is_not_evidence", want: "connection_unavailable", err: errors.New("401 device_offline SECRET")},
		{name: "offline", execution: "not_started", code: "device_offline", want: "offline"},
		{name: "runtime", execution: "rejected", code: "runtime_unavailable", want: "runtime_unavailable"},
		{name: "uncertain", execution: "unknown", code: "device_offline", want: "connection_unavailable"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			s, store, _ := serviceFixture(t)
			s.call = func(context.Context, string, string, string, map[string]any) (json.RawMessage, error) {
				if scenario.err != nil {
					return nil, scenario.err
				}
				return mcpValue(map[string]any{"execution": scenario.execution, "error": map[string]string{"code": scenario.code}}), nil
			}
			if got := Availability(s.Probe(t.Context(), "owner", store.record.RuntimeProviderConfig)); got != scenario.want {
				t.Fatalf("availability=%s", got)
			}
		})
	}
}
