package application

import (
	"context"
	"errors"
	"testing"
	"time"

	sdk "github.com/felinics/twilight/sdk"

	"github.com/felinics/memoh/internal/agent/runtime/native"
	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/agent/sessionmode"
	tools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/agent/toolexec"
	sessionpkg "github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/toolcontext"
)

type childComputerTools struct {
	t     *testing.T
	calls int
	run   string
}

func (p *childComputerTools) Tools(ctx context.Context, s tools.SessionContext) ([]toolexec.Tool, error) {
	p.calls++
	b, err := toolcontext.ResolveComputerBinding(ctx, toolcontext.Session{BotID: s.BotID, SessionID: s.SessionID, UserID: s.UserID})
	if err != nil || b.SessionID != lifecycleTestSessionID || b.TargetID != "pc-a" || b.RunID != p.run {
		p.t.Fatalf("actual spawned tool binding: %+v %v", b, err)
	}
	for _, target := range []string{"pc-b", "native"} {
		if !errors.Is(toolcontext.CheckComputerTarget(ctx, target), toolcontext.ErrComputerScope) {
			p.t.Fatal("child gained another computer")
		}
	}
	return nil, nil
}

func TestComputerBindingThroughRealSubagentAdmissionAndSpawn(t *testing.T) {
	for _, wrongParent := range []bool{false, true} {
		t.Run(map[bool]string{false: "persisted-child", true: "unrelated-thread"}[wrongParent], func(t *testing.T) {
			manager := sessionruntime.NewManager(sessionruntime.NewMemoryBackend(), sessionruntime.Options{OwnerID: "computer-child", OwnerLeaseTTL: time.Minute, Ledger: newAbortAlignmentLedger(), Fence: abortAlignmentFence{}})
			t.Cleanup(func() { _ = manager.Close() })
			service := &Service{contextLifecycles: &recordingContextLifecycleStore{}, sessionService: &fakeBackgroundSessionService{getFn: func(_ context.Context, id string) (sessionpkg.Thread, error) {
				parent := "parent-thread"
				if wrongParent {
					parent = "unrelated-parent"
				}
				return sessionpkg.Thread{ID: id, BotID: lifecycleTestBotID, Type: sessionmode.Subagent, ParentThreadID: parent}, nil
			}}}
			service.SetSessionRuntime(manager)
			parent, err := toolcontext.WithComputerBinding(t.Context(), toolcontext.ComputerBinding{BotID: lifecycleTestBotID, UserID: "user", SessionID: "parent-thread", TargetID: "pc-a", RunID: "parent-run"})
			if err != nil {
				t.Fatal(err)
			}
			runCtx, admission, finish, err := service.AdmitSubagentRun(context.WithoutCancel(parent), lifecycleTestBotID, lifecycleTestSessionID, "subagent:computer", []byte(`{"task":"synthetic"}`))
			if wrongParent {
				if !errors.Is(err, toolcontext.ErrComputerBinding) {
					t.Fatalf("unrelated admission: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			provider := &childComputerTools{t: t, run: admission.RunID}
			agent := native.New(native.Deps{})
			agent.SetToolProviders([]tools.ToolProvider{provider})
			adapter := native.NewSpawnAdapter(agent)
			adapter.SetRunObserverFactory(service.SubagentRunObserver)
			result, runErr := adapter.GenerateWithWatchdog(runCtx, tools.SpawnRunConfig{RunID: admission.RunID, SupportsToolCall: true, Model: &sdk.Model{ID: "synthetic", Type: sdk.ModelTypeChat, Provider: abortAlignmentProvider{complete: true}}, Query: "synthetic", Identity: tools.SpawnIdentity{BotID: lifecycleTestBotID, SessionID: lifecycleTestSessionID, UserID: "user", WorkspaceTargetID: "pc-a", WorkspaceTargetKind: "remote", IsSubagent: true}}, func() {})
			finish(tools.SubagentTerminal{Cause: runErr, ContextLifecycle: result.ContextLifecycle, OutcomeResolved: true, Outcome: tools.SpawnAttemptCompleted})
			if runErr != nil || provider.calls == 0 {
				t.Fatalf("real spawn: calls=%d error=%v", provider.calls, runErr)
			}
			b, _ := toolcontext.HostComputerBinding(parent)
			if b.SessionID != "parent-thread" {
				t.Fatal("parent binding mutated")
			}
		})
	}
}
