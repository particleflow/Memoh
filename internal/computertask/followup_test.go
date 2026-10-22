package computertask

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/mcp"
	"github.com/felinics/memoh/internal/toolcontext"
)

type followupTransport struct {
	t        *testing.T
	base     *transportFixture
	session  *agenvo.AgentSessionInfo
	fault    string
	afterGet func()
	onPrompt func()
	methods  []string
	prompts  []string
}

func (f *followupTransport) call(ctx context.Context, owner, connection, tool string, args map[string]any) (json.RawMessage, error) {
	f.t.Helper()
	var parts []json.RawMessage
	code := args["code"].(string)
	if err := json.Unmarshal([]byte("["+strings.TrimSuffix(strings.TrimPrefix(code, "return await call("), ");")+"]"), &parts); err != nil || len(parts) != 3 {
		f.t.Fatal("nonliteral native invocation")
	}
	var method string
	var params map[string]any
	if err := json.Unmarshal(parts[1], &method); err != nil {
		f.t.Fatal(err)
	}
	if err := json.Unmarshal(parts[2], &params); err != nil {
		f.t.Fatal(err)
	}
	f.methods = append(f.methods, method)
	raw, err := f.base.call(ctx, owner, connection, tool, args)
	if err != nil || method != "agent.get" && method != "agent.prompt" {
		return raw, err
	}
	if params["name"] != f.base.agentName || params["session"] != "synthetic" {
		f.t.Fatal("follow-up used a different native Agent or service session")
	}
	a := map[string]any{"name": f.base.agentName, "agent": "pi", "workspace_id": "w-synthetic", "pane_id": "p-agent", "terminal_id": "t-agent", "agent_status": "done", "interactive_ready": true, "completion_seq": 1}
	if f.session != nil {
		a["agent_session"] = f.session
	}
	if method == "agent.get" {
		if f.afterGet != nil {
			f.afterGet()
		}
		switch f.fault {
		case "missing":
			return envelope(map[string]any{"session": "synthetic", "nativeError": map[string]any{"execution": "rejected", "error": map[string]any{"code": "native_error", "native": map[string]any{"code": "agent_not_found", "message": "PRIVATE"}}}}), nil
		case "replaced":
			a["terminal_id"] = "another-terminal"
		case "blocked":
			a["agent_status"] = "blocked"
		case "starting":
			a["launch_pending"] = true
		case "wrong_session":
			return envelope(map[string]any{"session": "other", "result": map[string]any{"agent": a}}), nil
		}
		return envelope(map[string]any{"session": "synthetic", "result": map[string]any{"agent": a}}), nil
	}
	if f.onPrompt != nil {
		f.onPrompt()
	}
	f.prompts = append(f.prompts, params["text"].(string))
	switch f.fault {
	case "lost":
		return nil, errors.New("PRIVATE lost response after input")
	case "partial", "rejected":
		code := "agent_not_ready"
		if f.fault == "partial" {
			code = "agent_prompt_failed"
		}
		return envelope(map[string]any{"nativeError": map[string]any{"execution": "rejected", "error": map[string]any{"code": "native_error", "native": map[string]any{"code": code, "message": "PRIVATE"}}}}), nil
	case "session_during_prompt":
		a["agent_session"] = syntheticAgentSession("changed-during-prompt")
	case "metadata_lost_during_prompt":
		delete(a, "agent_session")
	case "reference_during_prompt":
		a["terminal_id"] = "replacement-terminal"
	case "malformed":
		return envelope(map[string]any{"session": "synthetic"}), nil
	}
	return envelope(map[string]any{"id": "cli:agent:prompt", "session": "synthetic", "result": map[string]any{"type": "agent_prompted", "agent": a}}), nil
}

func syntheticAgentSession(value string) *agenvo.AgentSessionInfo {
	return &agenvo.AgentSessionInfo{Source: "herdr:pi", Agent: "pi", Kind: "path", Value: `C:\Synthetic\PRIVATE\` + value + ".jsonl"}
}

func followupFixture(t *testing.T, session bool) (*Source, *fixtureStore, *followupTransport, Result) {
	t.Helper()
	source, store, base := nativeAgentBoundaryFixture(t)
	f := &followupTransport{t: t, base: base}
	if session {
		f.session = syntheticAgentSession("original")
	}
	source.service.relay = agenvo.NewService(store, permissionChecker{}, f.call)
	c := standardClient(t, source, "pc-a", false)
	r := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "synthetic original task"}, false))
	f.methods = nil
	f.prompts = nil
	return source, store, f, r
}

func followupArgs(id, mode string) map[string]any {
	return map[string]any{"operation_id": id, "request_id": uuid.NewString(), "mode": mode, "task": "synthetic next instruction"}
}

func TestNativeFollowupReusesAgentAndRetainsOperationHistory(t *testing.T) {
	source, store, f, initial := followupFixture(t, true)
	original := store.records[initial.ID]
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(original.Details, &fields); err != nil {
		t.Fatal(err)
	}
	fields["delivery_ledger"] = json.RawMessage(`{"synthetic_marker":"retained"}`)
	fields["future_metadata"] = json.RawMessage(`{"synthetic_marker":"retained"}`)
	original.Details, _ = json.Marshal(fields)
	store.records[initial.ID] = original
	c := standardClient(t, source, "pc-a", false)
	args := followupArgs(initial.ID, "bound_session")
	wire := invoke(t, c, "computer_task_followup", args, false)
	got := parseResult(t, wire)
	if got.ID != initial.ID || got.NativeAgentName != f.base.agentName || got.WorkspaceID != "w-synthetic" || got.PaneID != "p-agent" || got.Followup == nil || got.Followup.State != "accepted" || got.Followup.SessionRelation != "observed_match" || !got.Followup.PromptAccepted || got.Followup.BusinessOutcome != "unknown" || got.Followup.ContinuityVerified || got.Followup.AtomicIdentityGuaranteed || got.Followup.StrictResumeAvailable {
		t.Fatalf("incorrect follow-up facts: %+v", got)
	}
	if strings.Contains(string(wire), "PRIVATE") || !slices.Equal(f.methods, []string{"agent.get", "agent.prompt"}) {
		t.Fatalf("private session leaked or Agent rebuilt: %s %v", wire, f.methods)
	}
	before := observationSnapshot(t, store)
	// Reconstruct the real application service and use a new standard client.
	restarted := NewSource(NewService(store, store, source.service.relay, nil, source.service.authorize))
	c = standardClient(t, restarted, "pc-a", false)
	again := parseResult(t, invoke(t, c, "computer_task_followup", args, false))
	if again.Followup == nil || *again.Followup != *got.Followup || !slices.Equal(f.methods, []string{"agent.get", "agent.prompt"}) || !slices.Equal(before, observationSnapshot(t, store)) {
		t.Fatal("duplicate request did not reuse its durable outcome")
	}
	args["task"] = "different instruction under the same request ID"
	conflict := parseResult(t, invoke(t, c, "computer_task_followup", args, true))
	if !conflict.RequestConflict || !slices.Equal(before, observationSnapshot(t, store)) {
		t.Fatal("changed request content overwrote its original receipt")
	}
	second := parseResult(t, invoke(t, c, "computer_task_followup", followupArgs(initial.ID, "current_agent"), false))
	if len(second.Followups) != 2 || second.Followup.State != "accepted" || second.ID != initial.ID {
		t.Fatal("second explicit instruction lost the original operation")
	}
	saved := store.records[initial.ID]
	var updated map[string]json.RawMessage
	if err := json.Unmarshal(saved.Details, &updated); err != nil {
		t.Fatal(err)
	}
	for key, value := range fields {
		if string(updated[key]) != string(value) {
			t.Fatalf("follow-up changed original field %s", key)
		}
	}
	if saved.State != original.State || saved.Fingerprint != original.Fingerprint || saved.TargetFingerprint != original.TargetFingerprint || len(store.records) != 1 {
		t.Fatal("follow-up replaced original operation identity/outcome")
	}
}

func TestNativeFollowupSessionModesNeverSilentlyResume(t *testing.T) {
	for _, tc := range []struct {
		name, mode, relation, state string
		bound, present, changed     bool
	}{
		{"bound_match", "bound_session", "observed_match", "accepted", true, true, false},
		{"bound_changed", "bound_session", "changed", "rejected", true, true, true},
		{"bound_missing", "bound_session", "unavailable", "rejected", true, false, false},
		{"legacy_bound", "bound_session", "unbound", "rejected", false, true, false},
		{"current_changed", "current_agent", "changed", "accepted", true, true, true},
		{"current_missing", "current_agent", "unavailable", "accepted", true, false, false},
		{"legacy_current", "current_agent", "unbound", "accepted", false, true, false},
		{"no_extension", "current_agent", "unbound", "accepted", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, store, f, initial := followupFixture(t, tc.bound)
			f.session = nil
			if tc.present {
				f.session = syntheticAgentSession("original")
				if tc.changed {
					f.session = syntheticAgentSession("after-new-or-resume")
				}
			}
			var before Details
			if err := json.Unmarshal(store.records[initial.ID].Details, &before); err != nil {
				t.Fatal(err)
			}
			c := standardClient(t, source, "pc-a", false)
			args := followupArgs(initial.ID, tc.mode)
			got := parseResult(t, invoke(t, c, "computer_task_followup", args, tc.state == "rejected"))
			if got.Followup == nil || got.Followup.State != tc.state || got.Followup.SessionRelation != tc.relation || got.Followup.ContinuityVerified || got.Followup.StrictResumeAvailable {
				t.Fatalf("wrong session facts: %+v", got.Followup)
			}
			want := []string{"agent.get"}
			if tc.state == "accepted" {
				want = append(want, "agent.prompt")
			}
			if !slices.Equal(f.methods, want) {
				t.Fatalf("session guard unexpectedly sent input: %v", f.methods)
			}
			var after Details
			if err := json.Unmarshal(store.records[initial.ID].Details, &after); err != nil {
				t.Fatal(err)
			}
			old, _ := json.Marshal(before.Agent)
			now, _ := json.Marshal(after.Agent)
			if string(old) != string(now) {
				t.Fatal("new observation retroactively changed the historical session binding")
			}
		})
	}
}

func TestNativeFollowupUnknownNeverRedispatches(t *testing.T) {
	for _, fault := range []string{"lost", "partial", "malformed", "session_during_prompt", "metadata_lost_during_prompt", "reference_during_prompt"} {
		t.Run(fault, func(t *testing.T) {
			source, store, f, initial := followupFixture(t, true)
			f.fault = fault
			c := standardClient(t, source, "pc-a", false)
			args := followupArgs(initial.ID, "bound_session")
			wire := invoke(t, c, "computer_task_followup", args, true)
			got := parseResult(t, wire)
			if got.Followup == nil || got.Followup.State != "unknown" || got.Followup.PromptAccepted || !got.Followup.InputMayHaveBeenSent || strings.Contains(string(wire), "PRIVATE") {
				t.Fatalf("uncertain input lost honest facts: %s", wire)
			}
			before := observationSnapshot(t, store)
			f.fault = ""
			invoke(t, c, "computer_task_followup", args, true)
			blocked := parseResult(t, invoke(t, c, "computer_task_followup", followupArgs(initial.ID, "current_agent"), true))
			if blocked.BlockedByRequest != args["request_id"] || blocked.ID != initial.ID || !slices.Equal(before, observationSnapshot(t, store)) || !slices.Equal(f.methods, []string{"agent.get", "agent.prompt"}) {
				t.Fatal("unknown was reinterpreted as permission to send another prompt")
			}
		})
	}
}

func TestNativeFollowupRejectsMissingChangedOrUnreadyAgentBeforeInput(t *testing.T) {
	for _, fault := range []string{"missing", "replaced", "blocked", "starting", "wrong_session", "rejected"} {
		t.Run(fault, func(t *testing.T) {
			source, _, f, initial := followupFixture(t, true)
			f.fault = fault
			c := standardClient(t, source, "pc-a", false)
			args := followupArgs(initial.ID, "current_agent")
			got := parseResult(t, invoke(t, c, "computer_task_followup", args, true))
			if got.Followup == nil || got.Followup.State != "rejected" || got.Followup.InputMayHaveBeenSent {
				t.Fatalf("pre-input failure not recorded: %+v", got.Followup)
			}
			before := slices.Clone(f.methods)
			invoke(t, c, "computer_task_followup", args, true)
			want := []string{"agent.get"}
			if fault == "rejected" {
				want = append(want, "agent.prompt")
			}
			if !slices.Equal(f.methods, want) || !slices.Equal(f.methods, before) {
				t.Fatalf("rejected request was retried: %v", f.methods)
			}
		})
	}
}

type followupFaultStore struct {
	dbstore.ComputerOperationStore
	save func(context.Context, dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error)
}

func (s followupFaultStore) SaveComputerOperation(ctx context.Context, r dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
	return s.save(ctx, r)
}

func TestNativeFollowupPersistenceFailuresDoNotRepeatInput(t *testing.T) {
	for _, fault := range []string{"reservation_lost", "dispatch_save_failed", "dispatch_response_lost", "dispatch_response_lost_unsettled", "final_save_failed", "final_response_lost"} {
		t.Run(fault, func(t *testing.T) {
			source, store, f, initial := followupFixture(t, true)
			dispatchCommitted := false
			source.service.store = followupFaultStore{ComputerOperationStore: store, save: func(ctx context.Context, r dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
				var d Details
				if err := json.Unmarshal(r.Details, &d); err != nil {
					t.Fatal(err)
				}
				state := d.Followups[len(d.Followups)-1].Progress.State
				if fault == "dispatch_save_failed" && state == "dispatching" || fault == "final_save_failed" && state == "accepted" || fault == "dispatch_response_lost_unsettled" && state == "rejected" {
					return r, errors.New("PRIVATE synthetic persistence failure")
				}
				next, err := store.SaveComputerOperation(ctx, r)
				if strings.HasPrefix(fault, "dispatch_response_lost") && state == "dispatching" {
					if err != nil {
						t.Fatal(err)
					}
					// The dispatch intent really committed; only the response is lost.
					persisted, readErr := store.ReadComputerOperation(ctx, r)
					if readErr != nil || persisted.Revision != r.Revision+1 {
						t.Fatal("dispatching write did not commit")
					}
					var committed Details
					if json.Unmarshal(persisted.Details, &committed) != nil || len(committed.Followups) != 1 || committed.Followups[0].Progress.State != "dispatching" {
						t.Fatal("committed row did not contain dispatching intent")
					}
					dispatchCommitted = true
					return r, errors.New("PRIVATE committed dispatch save response lost")
				}
				if fault == "reservation_lost" && state == "reserved" || fault == "final_response_lost" && state == "accepted" {
					return r, errors.New("PRIVATE synthetic persistence response lost")
				}
				return next, err
			}}
			c := standardClient(t, source, "pc-a", false)
			args := followupArgs(initial.ID, "bound_session")
			want := "unknown"
			if fault == "dispatch_save_failed" || fault == "dispatch_response_lost" {
				want = "rejected"
			}
			if fault == "final_response_lost" {
				want = "accepted"
			}
			wire := invoke(t, c, "computer_task_followup", args, want != "accepted")
			got := parseResult(t, wire)
			if got.Followup == nil || got.Followup.State != want || strings.Contains(string(wire), "PRIVATE") {
				t.Fatalf("persistence failure reported false success: %s", wire)
			}
			calls := slices.Clone(f.methods)
			if strings.HasPrefix(fault, "dispatch_response_lost") && (!dispatchCommitted || !slices.Equal(calls, []string{"agent.get"})) {
				t.Fatal("lost dispatch-save acknowledgement authorized native input")
			}
			// Reconstruct from actual saved records, without a live worker or
			// the fault injector, then repeat the same intent UUID.
			restarted := NewSource(NewService(store, store, source.service.relay, nil, source.service.authorize))
			c = standardClient(t, restarted, "pc-a", false)
			invoke(t, c, "computer_task_followup", args, want != "accepted")
			if fault == "dispatch_response_lost_unsettled" {
				var saved Details
				if err := json.Unmarshal(store.records[initial.ID].Details, &saved); err != nil {
					t.Fatal(err)
				}
				if saved.Followups[0].Progress.State != "dispatching" {
					t.Fatal("unsettled committed intent was rewritten after restart")
				}
				blocked := parseResult(t, invoke(t, c, "computer_task_followup", followupArgs(initial.ID, "current_agent"), true))
				if blocked.BlockedByRequest != args["request_id"] {
					t.Fatal("new UUID bypassed unsettled dispatch intent")
				}
			}
			if !slices.Equal(calls, f.methods) {
				t.Fatal("storage recovery repeated native input")
			}
		})
	}
}

func TestNativeFollowupConcurrentRequestsHaveOneDispatchOwner(t *testing.T) {
	source, store, f, initial := followupFixture(t, true)
	entered, release := make(chan struct{}), make(chan struct{})
	f.afterGet = func() { close(entered); <-release }
	b := toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-a", RunID: "next-run", TargetID: "pc-a"}
	ctx, err := toolcontext.WithComputerBinding(t.Context(), b)
	if err != nil {
		t.Fatal(err)
	}
	in := Followup{OperationID: initial.ID, RequestID: uuid.NewString(), Mode: "bound_session", Task: "synthetic next instruction"}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		got, err := source.service.followup(ctx, b, in)
		if err != nil || got.Followup == nil || got.Followup.State != "accepted" {
			t.Errorf("dispatch owner: %+v %v", got.Followup, err)
		}
	}()
	<-entered
	for range 8 {
		duplicate, err := source.service.followup(ctx, b, in)
		if !errors.Is(err, agenvo.ErrUnknown) || duplicate.Followup == nil || duplicate.Followup.State != "unknown" {
			t.Error("duplicate did not preserve pending reservation")
		}
	}
	other := in
	other.RequestID = uuid.NewString()
	blocked, err := source.service.followup(ctx, b, other)
	if !errors.Is(err, ErrConflict) || blocked.BlockedByRequest != in.RequestID {
		t.Error("another request bypassed pending reservation")
	}
	close(release)
	wg.Wait()
	if len(store.records) != 1 || !slices.Equal(f.methods, []string{"agent.get", "agent.prompt"}) {
		t.Fatalf("concurrent requests dispatched twice: %v", f.methods)
	}
}

func TestNativeFollowupRechecksExistingAuthority(t *testing.T) {
	for _, change := range []string{"none", "nas", "user", "bot", "session", "selected_pc", "revoked", "grant_removed", "registration", "public", "idle", "expired", "forged_target", "policy"} {
		t.Run(change, func(t *testing.T) {
			source, store, f, initial := followupFixture(t, true)
			b := toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-a", RunID: "later-run", TargetID: "pc-a"}
			args := followupArgs(initial.ID, "bound_session")
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
				b.TargetID = "pc-b" // This other PC also has a grant.
			case "revoked":
				store.revoked = true
			case "grant_removed":
				delete(store.mounts, "pc-a")
			case "registration":
				r := store.mounts["pc-a"]
				var target agenvo.Target
				_ = json.Unmarshal(r.RuntimeProviderConfig, &target)
				target.Session = "changed-native-service"
				r.RuntimeProviderConfig, _ = json.Marshal(target)
				store.mounts["pc-a"] = r
			case "forged_target":
				args["target_id"] = "pc-b"
			case "policy":
				source.service.authorize = func(context.Context, toolcontext.ComputerBinding, string, string, string) error {
					return errors.New("PRIVATE policy cause")
				}
			}
			before := observationSnapshot(t, store)
			ctx, err := toolcontext.WithComputerBinding(t.Context(), b)
			if err != nil {
				t.Fatal(err)
			}
			if change == "expired" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			session := mcp.ToolSessionContext{BotID: b.BotID, UserID: b.UserID, SessionID: b.SessionID, RunID: b.RunID, RunContext: ctx, RequireActiveRun: true, RuntimeActive: change != "idle", PublicRequest: change == "public"}
			if change == "idle" {
				// The existing host mount revokes a turn through its guard;
				// RuntimeActive alone does not revoke a trusted RunContext.
				session.RuntimeGuard = func(context.Context) error { return ErrDenied }
			}
			out, callErr := source.CallTool(ctx, session, "computer_task_followup", args)
			wire, _ := json.Marshal(out)
			if strings.Contains(string(wire), "PRIVATE") {
				t.Fatal("private authorization cause leaked")
			}
			if change == "none" || change == "nas" {
				if callErr != nil || out["isError"] == true || !slices.Equal(f.methods, []string{"agent.get", "agent.prompt"}) {
					t.Fatalf("authorized follow-up failed: %v %s %v", callErr, wire, f.methods)
				}
			} else if callErr == nil && out["isError"] != true || len(f.methods) != 0 || !slices.Equal(before, observationSnapshot(t, store)) {
				t.Fatalf("unauthorized follow-up changed state or contacted provider: %s %v", wire, f.methods)
			}
		})
	}
}

func TestNativeFollowupAuthorityChangesDuringDispatch(t *testing.T) {
	for _, change := range []string{"grant_after_get", "policy_after_get", "cancel_after_get", "grant_after_prompt", "cancel_after_prompt"} {
		t.Run(change, func(t *testing.T) {
			source, store, f, initial := followupFixture(t, true)
			b := toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-a", RunID: "later-run", TargetID: "pc-a"}
			ctx, err := toolcontext.WithComputerBinding(t.Context(), b)
			if err != nil {
				t.Fatal(err)
			}
			authorized := true
			checks := 0
			source.service.authorize = func(_ context.Context, got toolcontext.ComputerBinding, target, kind, task string) error {
				checks++
				if got != b || target != "pc-a" || kind != "herdr_pi_task" || task != "synthetic next instruction" {
					t.Fatal("follow-up invented a separate authority mapping")
				}
				if !authorized {
					return ErrApproval
				}
				return nil
			}
			mutate := func() {
				switch {
				case strings.HasPrefix(change, "grant"):
					store.revoked = true
				case strings.HasPrefix(change, "policy"):
					authorized = false
				case strings.HasPrefix(change, "cancel"):
					if _, err := source.service.cancel(ctx, b, initial.ID); err != nil {
						t.Fatal(err)
					}
				}
			}
			if strings.HasSuffix(change, "get") {
				f.afterGet = mutate
			} else {
				f.onPrompt = mutate
			}
			in := Followup{OperationID: initial.ID, RequestID: uuid.NewString(), Mode: "bound_session", Task: "synthetic next instruction"}
			got, callErr := source.service.followup(ctx, b, in)
			var saved Details
			if err := json.Unmarshal(store.records[initial.ID].Details, &saved); err != nil {
				t.Fatal(err)
			}
			wantState := "rejected"
			wantCalls := []string{"agent.get"}
			if strings.HasSuffix(change, "prompt") {
				wantState = "accepted"
				wantCalls = append(wantCalls, "agent.prompt")
			}
			if len(saved.Followups) != 1 || saved.Followups[0].Progress.State != wantState || !slices.Equal(f.methods, wantCalls) {
				t.Fatalf("wrong durable dispatch facts: %+v %v", saved.Followups, f.methods)
			}
			if strings.HasPrefix(change, "grant") && (callErr == nil || got.ID != "") {
				t.Fatal("receipt released after revocation")
			}
			if strings.HasPrefix(change, "cancel") && (!store.records[initial.ID].CancelRequested || got.ProcessStopped) {
				t.Fatal("follow-up erased cancellation intent or claimed stop")
			}
			if change == "policy_after_get" && checks != 2 {
				t.Fatal("policy was not checked again immediately before dispatch")
			}
		})
	}
}

func TestNativeFollowupRejectsUnsupportedHistoryAndInput(t *testing.T) {
	for _, change := range []string{"batch", "initial_unknown", "cancel", "limit", "missing_mode", "invalid_id", "invalid_session"} {
		t.Run(change, func(t *testing.T) {
			source, store, f, initial := followupFixture(t, true)
			r := store.records[initial.ID]
			var d Details
			_ = json.Unmarshal(r.Details, &d)
			args := followupArgs(initial.ID, "bound_session")
			switch change {
			case "batch":
				d.Batch = &BatchDetails{Progress: agenvo.BatchProgress{PiSessionID: "synthetic-batch-id"}}
			case "initial_unknown":
				d.Agent.PromptSubmitted = false
				r.State = "unknown"
			case "cancel":
				r.CancelRequested = true
			case "limit":
				for range maxFollowups {
					d.Followups = append(d.Followups, FollowupRecord{RequestID: uuid.NewString(), Progress: agenvo.AgentFollowupProgress{State: "accepted"}})
				}
			case "missing_mode":
				delete(args, "mode")
			case "invalid_id":
				args["request_id"] = "caller-label"
			case "invalid_session":
				f.session = &agenvo.AgentSessionInfo{Source: "untrusted", Agent: "pi", Kind: "path", Value: "/PRIVATE/session"}
			}
			r.Details, _ = json.Marshal(d)
			store.records[initial.ID] = r
			c := standardClient(t, source, "pc-a", false)
			wire := invoke(t, c, "computer_task_followup", args, true)
			wantCalls := []string(nil)
			if change == "invalid_session" {
				wantCalls = []string{"agent.get"}
			}
			if !slices.Equal(f.methods, wantCalls) || strings.Contains(string(wire), "PRIVATE") {
				t.Fatalf("invalid follow-up reached native input or leaked metadata: %v", f.methods)
			}
		})
	}
}
