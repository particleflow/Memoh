package tools

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/felinics/memoh/internal/agent/background"
	"github.com/felinics/memoh/internal/agent/toolexec"
	thread "github.com/felinics/memoh/internal/chat/thread"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"sync"
	"testing"
	"time"
)

type nativeQueueFixture struct {
	mu            sync.Mutex
	rows          map[string]dbstore.NativeTaskReceipt
	parent        SessionContext
	writeErr      error
	finishUnknown bool
}

func (f *nativeQueueFixture) Lookup(_ context.Context, _ SessionContext, call, hash string) (dbstore.NativeTaskReceipt, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[call]
	if ok && r.CallFingerprint != hash {
		return r, false, errors.New("conflicting call")
	}
	return r, ok, nil
}
func (f *nativeQueueFixture) Enqueue(ctx context.Context, s SessionContext, call, agent, child string, cfg thread.SubagentConfig, msg, description string) (dbstore.NativeTaskReceipt, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writeErr != nil {
		return dbstore.NativeTaskReceipt{}, false, f.writeErr
	}
	if r, ok := f.rows[call]; ok {
		return r, false, nil
	}
	config, _ := json.Marshal(cfg)
	r := dbstore.NativeTaskReceipt{Version: 1, ID: call, TaskID: "durable-" + call, CallID: call, CallFingerprint: NativeTaskCallFingerprint(ctx), BotID: s.BotID, ParentSessionID: s.SessionID, SessionID: child, AgentID: agent, State: "queued", Message: msg, Description: description, Config: config}
	f.rows[call] = r
	f.parent = s
	return r, true, nil
}
func (f *nativeQueueFixture) Claim(_ context.Context, r dbstore.NativeTaskReceipt) (dbstore.NativeTaskReceipt, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	old := f.rows[r.CallID]
	if old.State != "queued" {
		return old, false, nil
	}
	old.State = "claimed"
	old.ClaimToken = "claim"
	f.rows[r.CallID] = old
	return old, true, nil
}
func (f *nativeQueueFixture) Finish(_ context.Context, r dbstore.NativeTaskReceipt, run, state string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.finishUnknown {
		state = "unknown"
	}
	r.State = state
	r.RunID = run
	f.rows[r.CallID] = r
	return state, nil
}
func (f *nativeQueueFixture) Reject(_ context.Context, r dbstore.NativeTaskReceipt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r.State = "failed"
	f.rows[r.CallID] = r
	return nil
}
func (f *nativeQueueFixture) Pending(context.Context) ([]dbstore.NativeTaskReceipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []dbstore.NativeTaskReceipt
	for _, r := range f.rows {
		if r.State == "queued" {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *nativeQueueFixture) RestoreSession(ctx context.Context, _ dbstore.NativeTaskReceipt) (context.Context, SessionContext, error) {
	return ctx, f.parent, nil
}

func nativeQueueCall(t *testing.T, p *SpawnProvider, s SessionContext, call, name string, args map[string]any) (any, error) {
	t.Helper()
	ts, e := p.Tools(context.Background(), s)
	if e != nil {
		t.Fatal(e)
	}
	for _, tool := range ts {
		if tool.Name == name {
			out, e := tool.Execute(&toolexec.ToolExecContext{Context: context.Background(), ToolCallID: call, ToolName: name}, toolexec.ArgumentsFromValue(args))
			return toolexec.OutputValue(out), e
		}
	}
	t.Fatal("missing tool")
	return nil, nil
}

func TestNativeTaskQueueReplayDoesNotCreateChildOrExecuteAgain(t *testing.T) {
	block := make(chan struct{})
	agent := &fakeSpawnAgent{block: block}
	p, bg, sessions, _ := newAgentControlProvider(t, agent)
	f := &nativeQueueFixture{rows: map[string]dbstore.NativeTaskReceipt{}}
	p.SetNativeTaskQueue(f)
	s := SessionContext{BotID: "bot1", SessionID: "parent1"}
	args := map[string]any{"task": "first", "run_in_background": true}
	out, e := nativeQueueCall(t, p, s, "call-1", "spawn_agent", args)
	if e != nil {
		t.Fatal(e)
	}
	id := asMap(t, out)["task_id"].(string)
	waitUntil(t, time.Second, func() bool { return len(agent.queries()) == 1 })
	again, e := nativeQueueCall(t, p, s, "call-1", "spawn_agent", args)
	if e != nil || asMap(t, again)["task_id"] != id {
		t.Fatalf("replay %v %v", again, e)
	}
	sessions.mu.Lock()
	n := len(sessions.sessions)
	sessions.mu.Unlock()
	if n != 1 {
		t.Fatalf("replay created %d children", n)
	}
	args["task"] = "different"
	if _, e = nativeQueueCall(t, p, s, "call-1", "spawn_agent", args); e == nil {
		t.Fatal("changed input replay accepted")
	}
	close(block)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	snap, _, e := bg.WaitForSessionTask(ctx, s.BotID, s.SessionID, id, 0)
	if e != nil || snap.Status != background.TaskCompleted || len(agent.queries()) != 1 {
		t.Fatalf("execution %+v %v", snap, e)
	}
}

func TestNativeTaskQueueFailureGatesExecutionAndUnknownClearsSuccess(t *testing.T) {
	agent := &fakeSpawnAgent{}
	p, bg, _, _ := newAgentControlProvider(t, agent)
	f := &nativeQueueFixture{rows: map[string]dbstore.NativeTaskReceipt{}, writeErr: errors.New("store unavailable")}
	p.SetNativeTaskQueue(f)
	s := SessionContext{BotID: "bot1", SessionID: "parent1"}
	args := map[string]any{"task": "work"}
	if _, e := nativeQueueCall(t, p, s, "denied", "spawn_agent", args); e == nil || len(agent.queries()) != 0 {
		t.Fatal("execution preceded durable admission")
	}
	f.writeErr = nil
	f.finishUnknown = true
	out, e := nativeQueueCall(t, p, s, "unknown", "spawn_agent", args)
	if e != nil {
		t.Fatal(e)
	}
	r := asMap(t, out)
	if r["status"] != "unknown" || r["text"] == "report for work" {
		t.Fatalf("uncertain execution reported success: %v", r)
	}
	if bg.Get(r["task_id"].(string)).Snapshot().AgentReport != "" {
		t.Fatal("unknown exposed success report")
	}
}

func TestNativeTaskQueueFreshProviderRestoresOnlyQueuedIdentity(t *testing.T) {
	s := SessionContext{BotID: "bot1", SessionID: "parent1"}
	f := &nativeQueueFixture{rows: map[string]dbstore.NativeTaskReceipt{}, parent: s}
	cfg := thread.SubagentConfig{ModelID: "test-model", ProviderName: "test-provider"}
	queued, _, _ := f.Enqueue(context.Background(), s, "queued", "worker", "child", cfg, "queued work", "Queued")
	unknown := queued
	unknown.CallID = "unknown"
	unknown.TaskID = "original-unknown"
	unknown.State = "unknown"
	f.rows[unknown.CallID] = unknown
	agent := &fakeSpawnAgent{}
	p, bg, _, _ := newAgentControlProvider(t, agent)
	p.SetNativeTaskQueue(f)
	if e := p.RecoverNativeTasks(context.Background()); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	snap, _, e := bg.WaitForSessionTask(ctx, s.BotID, s.SessionID, queued.TaskID, 0)
	if e != nil || snap.TaskID != queued.TaskID || snap.Status != background.TaskCompleted {
		t.Fatalf("restored task %+v %v", snap, e)
	}
	if e = p.RecoverNativeTasks(context.Background()); e != nil {
		t.Fatal(e)
	}
	if len(agent.queries()) != 1 || bg.Get(unknown.TaskID) != nil {
		t.Fatal("recovery duplicated or reexecuted unknown work")
	}
}
