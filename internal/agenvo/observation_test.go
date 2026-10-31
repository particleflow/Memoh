package agenvo

import (
	"context"
	"fmt"
	"testing"
)

func TestWorkspaceObservationBoundedProjection(t *testing.T) {
	for _, scenario := range []string{"large", "duplicate", "null", "private_status", "wrong_session"} {
		t.Run(scenario, func(t *testing.T) {
			s, _, f := serviceFixture(t)
			s.callNative = func(_ context.Context, _ Scope, _ Target, _ string, _ map[string]any) (Receipt, error) {
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
				return fixtureReceipt(map[string]any{"execution": "accepted", "result": map[string]any{"session": session, "result": map[string]any{"workspaces": items}}}), nil
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
