package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/felinics/memoh/internal/agent/toolexec"
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/toolcontext"
)

func TestFederationRetainsOwningComputerContext(t *testing.T) {
	for _, name := range []string{"computer_targets", "computer_task_submit"} {
		for _, scenario := range []string{"callback_without_values", "callback_canceled", "run_canceled", "missing_host", "other_session"} {
			t.Run(name+"/"+scenario, func(t *testing.T) {
				run, stopRun := context.WithCancel(t.Context())
				defer stopRun()
				binding := toolcontext.ComputerBinding{BotID: "bot", UserID: "user", SessionID: "session", TargetID: "pc", RunID: "run"}
				var err error
				if scenario != "missing_host" {
					run, err = toolcontext.WithComputerBinding(run, binding)
					if err != nil {
						t.Fatal(err)
					}
				}
				session := SessionContext{BotID: binding.BotID, UserID: binding.UserID, SessionID: binding.SessionID, WorkspaceTargetID: binding.TargetID}
				if scenario == "other_session" {
					session.SessionID = "forged-session"
				}
				// A nil task service stops at the boundary after binding. No task,
				// database, network or device operation can occur in this test.
				provider := NewFederationProvider(nil, computertask.NewSource(nil))
				list, err := provider.Tools(run, session)
				if err != nil {
					t.Fatal(err)
				}
				callback, stopCall := context.WithCancel(context.Background())
				defer stopCall()
				if scenario == "callback_canceled" {
					stopCall()
				}
				if scenario == "run_canceled" {
					stopRun()
				}
				found := false
				for _, tool := range list {
					if tool.Name != name {
						continue
					}
					found = true
					_, err = tool.Execute(&toolexec.ToolExecContext{Context: callback, ToolCallID: "synthetic-call"}, toolexec.ArgumentsFromValue(map[string]any{}))
					want := computertask.ErrDenied
					if scenario == "missing_host" || scenario == "other_session" {
						want = toolcontext.ErrComputerBinding
					}
					if scenario == "callback_canceled" || scenario == "run_canceled" {
						want = context.Canceled
					}
					if !errors.Is(err, want) {
						t.Fatalf("error=%v want=%v", err, want)
					}
				}
				if !found {
					t.Fatal("computer tool missing")
				}
			})
		}
	}
}
