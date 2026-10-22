package computertask

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/agenvo"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/mcp"
	"github.com/felinics/memoh/internal/toolcontext"
)

func nativeAgentBoundaryFixture(t *testing.T) (*Source, *fixtureStore, *transportFixture) {
	t.Helper()
	source, store, transport := setup(t)
	r := store.mounts["pc-a"]
	var target agenvo.Target
	if err := json.Unmarshal(r.RuntimeProviderConfig, &target); err != nil {
		t.Fatal(err)
	}
	target.Agent = agenvo.AgentProfile{Provider: "synthetic", Model: "synthetic", ConfigDir: `C:\Synthetic\Pi`}
	r.RuntimeProviderConfig, _ = json.Marshal(target)
	store.mounts["pc-a"] = r
	return source, store, transport
}

func TestNativeAgentLedgerReconstructionDoesNotResumePrompt(t *testing.T) {
	for _, lost := range []bool{false, true} {
		name := "accepted"
		if lost {
			name = "lost_receipt"
		}
		t.Run(name, func(t *testing.T) {
			source, store, transport := nativeAgentBoundaryFixture(t)
			source.service.relay = agenvo.NewService(store, permissionChecker{}, func(ctx context.Context, owner, connection, tool string, args map[string]any) (json.RawMessage, error) {
				raw, err := transport.call(ctx, owner, connection, tool, args)
				if lost && strings.Contains(args["code"].(string), `"agent.prompt"`) {
					// The synthetic provider applied input, but the host received no receipt.
					return nil, errors.New("synthetic lost response PRIVATE")
				}
				return raw, err
			})
			client := standardClient(t, source, "pc-a", false)
			args := map[string]any{"kind": "herdr_pi_task", "task": "synthetic original task"}
			initial := parseResult(t, invoke(t, client, "computer_task_submit", args, lost))
			wantState := "submitted"
			if lost {
				wantState = "unknown"
			}
			if initial.State != wantState || initial.ID == "" || initial.ProcessStopped || initial.Delivery != nil || initial.PiResult != nil {
				t.Fatalf("native receipt mixed with batch/process facts: %+v", initial)
			}
			var details Details
			if err := json.Unmarshal(store.records[initial.ID].Details, &details); err != nil {
				t.Fatal(err)
			}
			if details.Batch != nil || details.Agent.Name == "" || details.Agent.Name == initial.ID || details.Agent.WorkspaceID == "" || details.Agent.PaneID == "" || details.Agent.TerminalID == "" || details.Agent.PromptSubmitted == lost {
				t.Fatalf("lost or conflated native identity: %+v", details.Agent)
			}
			before := observationSnapshot(t, store)
			calls := slices.Clone(transport.calls)
			// Rebuild the service from serialized records, with no live worker.
			// This exercises application reconstruction, not PostgreSQL durability.
			restored := &fixtureStore{records: map[string]dbstore.ComputerOperationRecord{}, mounts: store.mounts}
			if err := json.Unmarshal(before, &restored.records); err != nil {
				t.Fatal(err)
			}
			relay := agenvo.NewService(restored, permissionChecker{}, transport.call)
			restarted := NewSource(NewService(restored, restored, relay, nil, source.service.authorize))
			client = standardClient(t, restarted, "pc-a", false)
			for _, tool := range []string{"computer_task_get", "computer_task_submit"} {
				input := args
				if tool == "computer_task_get" {
					input = map[string]any{"operation_id": initial.ID}
				}
				got := parseResult(t, invoke(t, client, tool, input, false))
				if got.ID != initial.ID || got.State != wantState {
					t.Fatalf("reconstruction replaced original receipt: %+v", got)
				}
			}
			conflict := parseResult(t, invoke(t, client, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "synthetic new follow-up intent"}, true))
			if conflict.ID != initial.ID || conflict.BlockedByOperation != initial.ID {
				t.Fatal("new task text bypassed the unsettled original operation")
			}
			if !slices.Equal(transport.calls, calls) || !bytes.Equal(before, observationSnapshot(t, restored)) {
				t.Fatal("reconstruction/retry recreated an Agent, sent input, or changed original tracking")
			}
			canceled := parseResult(t, invoke(t, client, "computer_task_cancel", map[string]any{"operation_id": initial.ID}, false))
			if !canceled.CancelRequested || canceled.ProcessStopped || canceled.State != "unknown" || !slices.Equal(transport.calls, calls) {
				t.Fatal("cancellation intent became native stop execution")
			}
		})
	}
}

func TestNativeAgentRecoveryRechecksOwnerSessionTargetAndGrant(t *testing.T) {
	for _, change := range []string{"none", "nas", "user", "bot", "session", "selected_pc", "revoked", "grant_removed", "registration_session"} {
		t.Run(change, func(t *testing.T) {
			source, store, transport := nativeAgentBoundaryFixture(t)
			client := standardClient(t, source, "pc-a", false)
			initial := parseResult(t, invoke(t, client, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "synthetic original task"}, false))
			var d Details
			if err := json.Unmarshal(store.records[initial.ID].Details, &d); err != nil {
				t.Fatal(err)
			}
			read := &observationTransport{t: t, refs: agenvo.BatchProgress{WorkspaceID: d.Agent.WorkspaceID, PaneID: d.Agent.PaneID, TerminalID: d.Agent.TerminalID}}
			source.service.relay = agenvo.NewService(store, permissionChecker{}, read.call)
			b := toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-a", TargetID: "pc-a", RunID: "later-run"}
			switch change {
			case "nas":
				b.TargetID = "native"
			case "user":
				b.UserID = "user-b"
			case "bot":
				b.BotID = "bot-b"
			case "session":
				b.SessionID = "thread-b"
			case "selected_pc":
				b.TargetID = "pc-b" // Both PCs have a grant; the active selection still restricts access.
			case "revoked":
				store.revoked = true
			case "grant_removed":
				delete(store.mounts, "pc-a")
			case "registration_session":
				r := store.mounts["pc-a"]
				var target agenvo.Target
				if err := json.Unmarshal(r.RuntimeProviderConfig, &target); err != nil {
					t.Fatal(err)
				}
				target.Session = "another-herdr-session"
				r.RuntimeProviderConfig, _ = json.Marshal(target)
				store.mounts["pc-a"] = r
			}
			before := observationSnapshot(t, store)
			ctx, err := toolcontext.WithComputerBinding(t.Context(), b)
			if err != nil {
				t.Fatal(err)
			}
			session := mcp.ToolSessionContext{BotID: b.BotID, UserID: b.UserID, SessionID: b.SessionID, RunID: b.RunID, RunContext: ctx, RequireActiveRun: true, RuntimeActive: true}
			out, err := source.CallTool(ctx, session, "computer_task_recover", map[string]any{"operation_id": initial.ID})
			if change != "none" && change != "nas" {
				if err == nil || out != nil || len(read.methods) != 0 {
					t.Fatal("scope change released an old receipt or contacted the provider")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				wire, err := json.Marshal(out["structuredContent"])
				if err != nil {
					t.Fatal(err)
				}
				var result struct {
					Computer Recovery `json:"computer"`
				}
				if err := json.Unmarshal(wire, &result); err != nil {
					t.Fatal(err)
				}
				got := result.Computer
				if got.OperationID != initial.ID || got.WorkspaceID != d.Agent.WorkspaceID || got.PaneID != d.Agent.PaneID || got.PiSessionID != "" || got.Attempt != "" || got.ProcessState != "unknown" || got.BusinessOutcome != "unknown" {
					t.Fatalf("recovery lost original references or invented batch facts: %+v", got)
				}
				if !slices.Equal(read.methods, []string{"workspace.list", "pane.get", "agent.get"}) {
					t.Fatalf("recovery was not read-only: %v", read.methods)
				}
			}
			if !bytes.Equal(before, observationSnapshot(t, store)) || len(transport.calls) != 5 {
				t.Fatal("scope verification modified the ledger or resubmitted the original task")
			}
		})
	}
}
