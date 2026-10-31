package application

import (
	"context"
	"errors"
	"testing"
	"time"

	toolapproval "github.com/felinics/memoh/internal/agent/decision/approval"
	userinput "github.com/felinics/memoh/internal/agent/decision/input"
	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	tools "github.com/felinics/memoh/internal/agent/tool"
	sessionpkg "github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/toolcontext"
	"github.com/felinics/memoh/internal/workdir"
)

func runComputerBindingEntry(ctx context.Context, service *Service, entry, user string) error {
	switch entry {
	case "message":
		_, err := service.StreamChatWS(ctx, ChatRequest{
			BotID: lifecycleTestBotID, ChatID: lifecycleTestBotID,
			ThreadID: lifecycleTestSessionID, RunID: lifecycleTestRunID, UserID: user, SourceChannelIdentityID: user,
			Query: "synthetic binding check", WorkspaceTargetID: "pc-synthetic",
		}, make(chan WSStreamEvent, 64), nil)
		return err
	case "answer":
		return service.continueUserInputSession(ctx, userinput.Request{
			BotID:     lifecycleTestBotID,
			SessionID: lifecycleTestSessionID, ChannelIdentityID: user, WorkspaceTargetID: "pc-synthetic",
		},
			UserInputResponseInput{BotID: lifecycleTestBotID, ActorUserID: "different-responder"}, lifecycleTestRunID, sessionruntime.RunHandle{}, nil, nil)
	default:
		return service.continueToolApprovalSession(ctx, toolapproval.Request{
			BotID:     lifecycleTestBotID,
			SessionID: lifecycleTestSessionID, ChannelIdentityID: user, WorkspaceTargetID: "pc-synthetic",
		},
			ToolApprovalResponseInput{BotID: lifecycleTestBotID, ActorUserID: "different-responder"}, lifecycleTestRunID, sessionruntime.RunHandle{}, nil, nil)
	}
}

func TestPostgresAcceptedRecoveryComputerBinding(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		name := "original_identity"
		if revoked {
			name = "target_revoked"
		}
		t.Run(name, func(t *testing.T) {
			f := newAcceptedInputRecoveryFixture(t)
			if _, err := f.pool.Exec(t.Context(), `UPDATE models SET config='{"context_window":128000,"compatibilities":["tool-call"]}' WHERE id=(SELECT (input_json->'resume'->>'model')::uuid FROM session_runs WHERE run_id=$1)`, f.input.OriginalRunID); err != nil {
				t.Fatal(err)
			}
			f.service.modelsService = models.NewService(f.service.logger, f.store)
			f.service.sessionService = sessionWorkdirSessionService{thread: sessionpkg.Thread{
				ID: f.input.SessionID, BotID: f.input.BotID, RuntimeType: sessionpkg.RuntimeModel, WorkdirID: "synthetic-workdir",
			}}
			f.service.workdirs = fakeSessionWorkdirResolver{resolved: workdir.Resolved{
				WorkdirID: "synthetic-workdir", TargetID: "pc-synthetic", Kind: workdir.TargetKindRemote, WorkDir: "/synthetic/work",
			}}
			f.service.workspaceTargets = scheduleWorkspaceTargets{targetID: "pc-synthetic", kind: workdir.TargetKindRemote}
			f.service.botPermissions = workspaceRequestPermission(true)
			if revoked {
				f.service.workspaceTargets = scheduleWorkspaceTargets{err: errors.New("synthetic revoked target")}
			}
			probe := &scheduleWorkspaceBindingProbe{}
			f.service.agent.SetToolProviders([]tools.ToolProvider{probe})
			result, err := f.service.RecoverAcceptedUserInput(t.Context(), f.input)
			if err != nil || !result.Started {
				t.Fatalf("recovery admission: %+v %v", result, err)
			}
			deadline := time.NewTimer(10 * time.Second)
			defer deadline.Stop()
			tick := time.NewTicker(10 * time.Millisecond)
			defer tick.Stop()
			for {
				run, err := f.store.GetSessionRun(t.Context(), db.ParseUUIDOrEmpty(result.RecoveryRunID))
				if err != nil {
					t.Fatal(err)
				}
				if run.State == "completed" || run.State == "failed" {
					if (run.State == "failed") != revoked {
						t.Fatalf("unexpected recovery state %s", run.State)
					}
					break
				}
				select {
				case <-deadline.C:
					t.Fatal("recovery did not settle")
				case <-tick.C:
				}
			}
			probe.mu.Lock()
			defer probe.mu.Unlock()
			if revoked {
				if probe.calls != 0 || len(f.requests.all()) != 0 {
					t.Fatal("revoked target reached model/tools")
				}
				return
			}
			want := toolcontext.ComputerBinding{
				BotID: f.input.BotID, UserID: f.input.ActorID,
				SessionID: f.input.SessionID, TargetID: "pc-synthetic", RunID: result.RecoveryRunID,
			}
			if probe.calls != 1 || probe.bindingErr != nil || probe.binding != want || len(f.requests.all()) != 1 {
				t.Fatalf("recovery binding=%+v error=%v calls=%d", probe.binding, probe.bindingErr, probe.calls)
			}
		})
	}
}

func TestNativeDecisionComputerBinding(t *testing.T) {
	for _, entry := range []string{"message", "answer", "approval"} {
		t.Run(entry, func(t *testing.T) {
			fixture, network := scheduleWorkspaceMemoryFixture(t)
			provider := configureTriggerLifecycleContextView(t, fixture, nil)
			probe := &scheduleWorkspaceBindingProbe{}
			fixture.service.agent.SetToolProviders([]tools.ToolProvider{probe})
			if err := runComputerBindingEntry(t.Context(), fixture.service, entry, "synthetic-user"); err != nil {
				t.Fatal(err)
			}
			probe.mu.Lock()
			defer probe.mu.Unlock()
			want := toolcontext.ComputerBinding{
				BotID: lifecycleTestBotID, UserID: "synthetic-user",
				SessionID: lifecycleTestSessionID, TargetID: "pc-synthetic", RunID: lifecycleTestRunID,
			}
			if probe.calls != 1 || probe.bindingErr != nil || probe.binding != want {
				t.Fatalf("native tool assembly binding=%+v error=%v calls=%d", probe.binding, probe.bindingErr, probe.calls)
			}
			if provider.callCount() != 1 || network.calls.Load() != 0 {
				t.Fatal("unexpected synthetic provider or network count")
			}
		})
	}
}

func TestNativeDecisionComputerBindingRejectsUnsafeContext(t *testing.T) {
	for _, entry := range []string{"message", "answer", "approval"} {
		for _, scenario := range []string{"missing_identity", "permission_revoked", "target_revoked", "other_session"} {
			t.Run(entry+"/"+scenario, func(t *testing.T) {
				fixture, network := scheduleWorkspaceMemoryFixture(t)
				provider := configureTriggerLifecycleContextView(t, fixture, nil)
				probe := &scheduleWorkspaceBindingProbe{}
				fixture.service.agent.SetToolProviders([]tools.ToolProvider{probe})
				ctx, user := t.Context(), "synthetic-user"
				switch scenario {
				case "missing_identity":
					user = ""
				case "permission_revoked":
					fixture.service.botPermissions = workspaceRequestPermission(false)
				case "target_revoked":
					fixture.service.workspaceTargets = scheduleWorkspaceTargets{err: errors.New("synthetic revoked target")}
				case "other_session":
					var err error
					ctx, err = toolcontext.WithComputerBinding(ctx, toolcontext.ComputerBinding{
						BotID:  lifecycleTestBotID,
						UserID: user, SessionID: "another-session", TargetID: "pc-synthetic", RunID: lifecycleTestRunID,
					})
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := runComputerBindingEntry(ctx, fixture.service, entry, user); err == nil {
					t.Fatal("unsafe continuation accepted")
				}
				if probe.calls != 0 || provider.callCount() != 0 || network.calls.Load() != 0 {
					t.Fatal("unsafe context reached native tools or model")
				}
			})
		}
	}
}
