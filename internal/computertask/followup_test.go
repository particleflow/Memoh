package computertask

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/toolcontext"
)

type followupTransport struct {
	t       *testing.T
	base    *transportFixture
	session *agenvo.AgentSessionInfo
	methods []string
}

func (f *followupTransport) call(_ context.Context, _ agenvo.Scope, _ agenvo.Target, method string, _ map[string]any) (agenvo.Receipt, error) {
	f.methods = append(f.methods, method)
	if method != "agent.get" && method != "agent.read" {
		f.t.Fatalf("historical observation attempted mutation %s", method)
	}
	a := map[string]any{"name": f.base.agentName, "agent": "pi", "workspace_id": "w-synthetic", "pane_id": "p-agent", "terminal_id": "t-agent", "agent_status": "done", "interactive_ready": true, "completion_seq": 1}
	if f.session != nil {
		a["agent_session"] = f.session
	}
	if method == "agent.read" {
		return envelope(map[string]any{"session": "synthetic", "output": "unverified Pi output"}), nil
	}
	return envelope(map[string]any{"session": "synthetic", "result": map[string]any{"agent": a}}), nil
}

func followupFixture(t *testing.T, session bool) (*Source, *fixtureStore, *followupTransport, Result) {
	t.Helper()
	source, store, base := nativeAgentBoundaryFixture(t)
	f := &followupTransport{t: t, base: base}
	if session {
		f.session = syntheticAgentSession("original")
	}
	source.service.relay = agenvo.NewService(store, permissionChecker{}, f.call)
	base.agentName = "historical-agent"
	progress := agenvo.AgentProgress{Name: base.agentName, WorkspaceID: "w-synthetic", PaneID: "p-agent", TerminalID: "t-agent", Phase: "prompt_submitted", PromptSubmitted: true, AgentSession: f.session}
	details := Details{Kind: "herdr_pi_task", Phase: progress.Phase, Agent: progress}
	raw, _ := json.Marshal(details)
	target := fixtureTarget(store.mounts["pc-a"])
	row := dbstore.ComputerOperationRecord{ID: uuid.NewString(), BotID: "bot-a", SessionID: "thread-a", UserID: "user-a", TargetID: "pc-a", RunID: "run-a", State: "unknown", TargetFingerprint: agenvo.TargetFingerprint(target), Details: raw}
	store.records[row.ID] = row
	r := result(row, details)

	f.methods = nil
	return source, store, f, r
}

func followupArgs(id, mode string) map[string]any {
	return map[string]any{"operation_id": id, "request_id": uuid.NewString(), "mode": mode, "task": "synthetic next instruction"}
}

func TestRetiredFollowupRetainsReceiptsWithoutWritingOrDispatch(t *testing.T) {
	for _, state := range []string{"accepted", "rejected", "unknown", "dispatching", "reserved"} {
		t.Run(state, func(t *testing.T) {
			source, store, transport, initial := followupFixture(t, true)
			b := toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-a", TargetID: "pc-a", RunID: "read-run"}
			in := Followup{OperationID: initial.ID, RequestID: uuid.NewString(), Mode: "bound_session", Task: "original input"}
			row := store.records[initial.ID]
			var d Details
			_ = json.Unmarshal(row.Details, &d)
			d.Followups = []FollowupRecord{{RequestID: in.RequestID, Fingerprint: fingerprint(in), Mode: in.Mode, Progress: agenvo.AgentFollowupProgress{State: state, Reason: "original_receipt", SessionRelation: "observed_match"}}}
			row.Details, _ = json.Marshal(d)
			store.records[row.ID] = row
			before := observationSnapshot(t, store)
			got, _ := source.service.followup(t.Context(), b, in)
			if got.Followup == nil || got.Followup.State != followupResult(d.Followups[0]).State || !slices.Equal(before, observationSnapshot(t, store)) || len(transport.methods) != 0 {
				t.Fatal("historical input receipt changed or dispatched", got)
			}
			in.Task = "changed intent"
			if _, err := source.service.followup(t.Context(), b, in); !errors.Is(err, ErrInvalid) {
				t.Fatal("changed replay intent accepted", err)
			}
			in.RequestID = uuid.NewString()
			if _, err := source.service.followup(t.Context(), b, in); !errors.Is(err, agenvo.ErrUnsupported) {
				t.Fatal("retired input accepted", err)
			}
			if !slices.Equal(before, observationSnapshot(t, store)) || len(transport.methods) != 0 {
				t.Fatal("retired input wrote or dispatched")
			}
		})
	}
}

func syntheticAgentSession(value string) *agenvo.AgentSessionInfo {
	return &agenvo.AgentSessionInfo{Source: "herdr:pi", Agent: "pi", Kind: "path", Value: `C:\Synthetic\PRIVATE\` + value + ".jsonl"}
}

type followupFaultStore struct {
	dbstore.ComputerOperationStore
	save func(context.Context, dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error)
}

func (s followupFaultStore) SaveComputerOperation(ctx context.Context, r dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
	return s.save(ctx, r)
}
