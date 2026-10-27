package computertask

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/toolcontext"
)

type observationTransport struct {
	t       *testing.T
	refs    agenvo.BatchProgress
	methods []string
	fault   string
	after   func()
}

func (f *observationTransport) call(_ context.Context, _ agenvo.Scope, _ agenvo.Target, method string, params map[string]any) (agenvo.Receipt, error) {
	f.t.Helper()

	f.methods = append(f.methods, method)
	if params["session"] != "synthetic" {
		f.t.Fatal("wrong native session")
	}
	if f.after != nil {
		f.after()
	}
	if f.fault == "authentication" {
		return agenvo.Receipt{}, errors.Join(agenvo.ErrAuthenticationUnavailable, errors.New("PRIVATE token"))
	}
	if f.fault == "connection" {
		return agenvo.Receipt{}, errors.New("PRIVATE 401 device_offline")
	}
	if f.fault == "offline" || f.fault == "runtime_unavailable" {
		code := f.fault
		if code == "offline" {
			code = "device_offline"
		}
		v, _ := json.Marshal(map[string]any{"execution": "not_started", "error": map[string]any{"code": code, "private": "SECRET"}})
		var receipt agenvo.Receipt
		_ = json.Unmarshal(v, &receipt)
		err := agenvo.ErrNativeUnavailable
		if code == "device_offline" {
			err = agenvo.ErrDeviceOffline
		}
		return receipt, errors.Join(agenvo.ErrRejected, err)
	}
	var body any
	switch method {
	case "workspace.list":
		items := []any{map[string]any{"workspace_id": f.refs.WorkspaceID, "label": "Synthetic workspace", "pane_count": 2, "agent_status": "done", "tokens": "SECRET", "worktree": map[string]string{"path": "PRIVATE"}}}
		if f.fault == "workspace_missing" {
			items = []any{}
		}
		body = map[string]any{"workspaces": items}
	case "pane.get":
		pane, terminal := f.refs.PaneID, f.refs.TerminalID
		if params["paneId"] == f.refs.ControlPane {
			pane, terminal = f.refs.ControlPane, f.refs.ControlTerminal
		}
		if params["paneId"] != pane {
			f.t.Fatal("caller-supplied pane")
		}
		if f.fault == "pane_missing" || f.fault == "control_missing" && pane == f.refs.ControlPane {
			return envelope(map[string]any{"nativeError": map[string]any{"execution": "rejected", "error": map[string]any{"code": "native_error", "native": map[string]string{"code": "pane_not_found", "message": "SECRET"}}}}), nil
		}
		if f.fault == "mismatch" {
			terminal = "other-invocation"
		}
		body = map[string]any{"pane": map[string]any{"pane_id": pane, "terminal_id": terminal, "workspace_id": f.refs.WorkspaceID, "cwd": "PRIVATE", "tokens": "SECRET"}}
	case "agent.get":
		if params["name"] != f.refs.PaneID {
			f.t.Fatal("recovery relied on volatile agent alias")
		}
		body = map[string]any{"agent": map[string]any{"pane_id": f.refs.PaneID, "terminal_id": f.refs.TerminalID, "workspace_id": f.refs.WorkspaceID, "agent_status": "done", "agent_session": map[string]string{"value": "PRIVATE"}}}
	default:
		f.t.Fatalf("observation dispatched non-read method %s", method)
	}
	session := "synthetic"
	if f.fault == "wrong_session" {
		session = "foreign"
	}
	return envelope(map[string]any{"session": session, "result": body}), nil
}

func recoveryFixture(t *testing.T) (*Source, *fixtureStore, *observationTransport, string) {
	t.Helper()
	source, store, _, _ := setupBatch(t)
	c := standardClient(t, source, "pc-a", false)
	r := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "synthetic 42"}, false))
	got := batchResultUntilReady(t, c, r.ID)
	if got.PiResult == nil || got.PiResult.Outcome != "succeeded" {
		t.Fatal("missing fixture result")
	}
	var d Details
	if err := json.Unmarshal(store.records[r.ID].Details, &d); err != nil {
		t.Fatal(err)
	}
	f := &observationTransport{t: t, refs: d.Batch.Progress}
	source.service.relay = agenvo.NewService(store, permissionChecker{}, f.call)
	return source, store, f, r.ID
}

func observationSnapshot(t *testing.T, store *fixtureStore) []byte {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	raw, err := json.Marshal(store.records)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestReadOnlyRecoveryPreservesVerifiedResultsAndOriginalRecords(t *testing.T) {
	for _, fault := range []string{"", "authentication", "connection", "offline", "runtime_unavailable", "workspace_missing", "pane_missing", "control_missing", "mismatch", "wrong_session"} {
		t.Run(fault, func(t *testing.T) {
			source, store, f, id := recoveryFixture(t)
			f.fault = fault
			before := observationSnapshot(t, store)
			c := standardClient(t, source, "pc-a", false)
			raw := invoke(t, c, "computer_task_recover", map[string]any{"operation_id": id}, false)
			var wire struct {
				Computer Recovery `json:"computer"`
			}
			if err := json.Unmarshal(raw, &wire); err != nil {
				t.Fatal(err)
			}
			got := wire.Computer
			if got.PiResult != "succeeded" || got.ProcessState != "exited" || got.BusinessOutcome != "unknown" || got.ResultReason != "verified_persisted_result" || got.ResultSummary == nil {
				t.Fatalf("lost verified facts: %+v", got)
			}
			if !bytes.Equal(before, observationSnapshot(t, store)) || strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), "PRIVATE") {
				t.Fatal("read mutated operation or exposed unprojected data")
			}
			availability := map[string]string{"authentication": "authentication_unavailable", "connection": "connection_unavailable", "offline": "offline", "runtime_unavailable": "runtime_unavailable"}[fault]
			if availability != "" && got.Native.Availability != availability {
				t.Fatalf("availability=%s", got.Native.Availability)
			}
			reason := map[string]string{"workspace_missing": "workspace_missing", "pane_missing": "execution_pane_missing", "control_missing": "control_pane_missing", "mismatch": "execution_pane_mismatch"}[fault]
			if reason != "" && got.Native.Reason != reason {
				t.Fatalf("reason=%s", got.Native.Reason)
			}
			if fault == "" && (got.Native.Match != "observed" || got.Native.AgentStatus != "done") {
				t.Fatalf("original references not observed: %+v", got.Native)
			}
		})
	}
}

func TestReadOnlyRecoveryNeverInfersCompletionOrRewritesLegacyReceipt(t *testing.T) {
	for _, mode := range []string{"missing", "legacy", "mismatch", "cancel", "invalid_identity"} {
		t.Run(mode, func(t *testing.T) {
			source, store, f, id := recoveryFixture(t)
			r := store.records[id]
			var d Details
			_ = json.Unmarshal(r.Details, &d)
			switch mode {
			case "legacy":
				d.Batch.Progress.CompletionVersion = 0
				d.Batch.Receipt.Completion = nil
				d.Batch.Result = nil
			case "mismatch":
				d.Batch.Receipt.Attempt = strings.Repeat("b", 32)
			case "invalid_identity":
				d.Batch.Progress.Attempt = "SECRET"
				d.Batch.Progress.PiSessionID = `C:\PRIVATE\session.jsonl`
			default:
				d.Batch.Receipt = nil
				r.State = "unknown"
			}
			if mode == "cancel" {
				r.CancelRequested = true
			}
			r.Details, _ = json.Marshal(d)
			store.records[id] = r
			before := observationSnapshot(t, store)
			raw := invoke(t, standardClient(t, source, "pc-a", false), "computer_task_recover", map[string]any{"operation_id": id}, false)
			var wire struct {
				Computer Recovery `json:"computer"`
			}
			_ = json.Unmarshal(raw, &wire)
			got := wire.Computer
			if mode == "invalid_identity" && (got.Attempt != "" || got.PiSessionID != "" || strings.Contains(string(raw), "PRIVATE") || strings.Contains(string(raw), "SECRET")) {
				t.Fatal("malformed identity reflected private text")
			}
			if got.PiResult != "unknown" || got.BusinessOutcome != "unknown" || got.ResultSummary != nil || got.Native.AgentStatus != "done" || !bytes.Equal(before, observationSnapshot(t, store)) {
				t.Fatalf("fabricated new receipt from native done: %+v", got)
			}
			if mode == "cancel" && (!got.CancelRequested || got.ProcessState != "unknown") {
				t.Fatal("cancel intent became termination")
			}
			if mode == "legacy" && (got.ProcessState != "exited" || got.ResultReason != "legacy_result_protocol") {
				t.Fatal("legacy process fact lost")
			}
			if len(f.methods) != 4 {
				t.Fatal("unexpected observation calls")
			}
		})
	}
}

func TestWorkspaceDiscoveryAndRecoveryRejectScopeAndLateRevocation(t *testing.T) {
	source, store, f, id := recoveryFixture(t)
	b := toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-a", TargetID: "pc-a", RunID: "run-a"}
	for _, field := range []string{"bot", "user", "session", "target", "revoked", "fingerprint"} {
		t.Run(field, func(t *testing.T) {
			bad := b
			mount := store.mounts["pc-a"]
			switch field {
			case "bot":
				bad.BotID = "bot-b"
			case "user":
				bad.UserID = "user-b"
			case "session":
				bad.SessionID = "thread-b"
			case "target":
				bad.TargetID = "pc-b"
			case "revoked":
				store.revoked = true
			case "fingerprint":
				target := fixtureTarget(mount)
				target.Session = "other"
				m := mount
				m.RuntimeProviderConfig = fixtureConfig(target)
				store.mounts["pc-a"] = m
			}
			before := len(f.methods)
			_, err := source.service.Recover(t.Context(), bad, id)
			store.revoked = false
			store.mounts["pc-a"] = mount
			if err == nil || len(f.methods) != before {
				t.Fatal("foreign observation reached PC")
			}
		})
	}
	c := standardClient(t, source, "pc-a", false)
	before := observationSnapshot(t, store)
	invoke(t, c, "computer_workspaces", map[string]any{"target_id": "pc-b"}, true)
	invoke(t, c, "computer_task_recover", map[string]any{"operation_id": id, "pane_id": "forged"}, true)
	listing := invoke(t, c, "computer_workspaces", map[string]any{}, false)
	if !strings.Contains(string(listing), "Synthetic workspace") || strings.Contains(string(listing), "PRIVATE") || strings.Contains(string(listing), "SECRET") || !bytes.Equal(before, observationSnapshot(t, store)) {
		t.Fatal("bad workspace projection")
	}
	for _, tool := range []string{"computer_workspaces", "computer_task_recover"} {
		store.revoked = false
		f.after = func() { store.revoked = true }
		args := map[string]any{}
		if tool == "computer_task_recover" {
			args["operation_id"] = id
		}
		invoke(t, c, tool, args, true)
	}
	store.revoked = false
	f.after = nil
	// NAS may choose an explicitly authorized PC, never an ungranted one.
	nas := standardClient(t, source, "native", false)
	invoke(t, nas, "computer_workspaces", map[string]any{"target_id": "pc-a"}, false)
	invoke(t, nas, "computer_workspaces", map[string]any{"target_id": "missing"}, true)
}
