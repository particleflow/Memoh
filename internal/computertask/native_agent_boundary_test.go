package computertask

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/mcp"
	"github.com/felinics/memoh/internal/toolcontext"
)

func nativeAgentBoundaryFixture(t *testing.T) (*Source, *fixtureStore, *transportFixture) {
	t.Helper()
	source, store, transport := setup(t)
	r := store.mounts["pc-a"]
	target := fixtureTarget(r)
	target.Agent = agenvo.AgentProfile{ExecutionMode: "pi-json-v1", Provider: "synthetic", Model: "synthetic", ConfigDir: `C:\Synthetic\Pi`}
	r.RuntimeProviderConfig = fixtureConfig(target)
	store.mounts["pc-a"] = r
	return source, store, transport
}

func TestNativeAgentRecoveryRechecksOwnerSessionTargetAndGrant(t *testing.T) {
	for _, change := range []string{"none", "nas", "user", "bot", "session", "selected_pc", "revoked", "grant_removed", "registration_session"} {
		t.Run(change, func(t *testing.T) {
			source, store, _, initial := followupFixture(t, true)
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
				target := fixtureTarget(r)
				target.Session = "another-herdr-session"
				r.RuntimeProviderConfig = fixtureConfig(target)
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
			if !bytes.Equal(before, observationSnapshot(t, store)) {
				t.Fatal("scope verification modified the ledger or resubmitted the original task")
			}
		})
	}
}
