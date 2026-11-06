package application

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	tools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/agent/toolexec"
	sessionpkg "github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/schedule"
	"github.com/felinics/memoh/internal/settings"
	"github.com/felinics/memoh/internal/toolcontext"
	"github.com/felinics/memoh/internal/workdir"
	"github.com/felinics/memoh/internal/workspace"
)

// Exercise the schedule entry and native tool assembly without network or devices.
// It installs no ComputerBinding on the scheduled input and never constructs
// a computer task service, starts a listener, or invokes a real provider.
type scheduleWorkspaceRejectNetwork struct{ calls atomic.Int64 }

func (r *scheduleWorkspaceRejectNetwork) RoundTrip(*http.Request) (*http.Response, error) {
	r.calls.Add(1)
	return nil, errors.New("synthetic fixture forbids network")
}

type scheduleWorkspaceTargets struct {
	targetID, kind string
	err            error
}

func (r scheduleWorkspaceTargets) ResolveWorkspaceTarget(ctx context.Context, _, target string) (workspace.ResolvedWorkspaceTarget, error) {
	if target == "" {
		target = workspace.WorkspaceTargetFromContext(ctx)
	}
	if r.err != nil {
		return workspace.ResolvedWorkspaceTarget{}, r.err
	}
	if target != r.targetID {
		return workspace.ResolvedWorkspaceTarget{}, errors.New("synthetic fixture requires the stored workdir target")
	}
	return workspace.ResolvedWorkspaceTarget{TargetID: target, Kind: r.kind, Name: "Synthetic workspace"}, nil
}

type scheduleWorkspaceBindingProbe struct {
	mu         sync.Mutex
	calls      int
	session    tools.SessionContext
	bindingErr error
	binding    toolcontext.ComputerBinding
	sourceErr  error
}

func (p *scheduleWorkspaceBindingProbe) Tools(ctx context.Context, session tools.SessionContext) ([]toolexec.Tool, error) {
	identity := toolcontext.Session{
		BotID: session.BotID, SessionID: session.SessionID, UserID: session.UserID,
		WorkspaceTargetID: session.WorkspaceTargetID, RunContext: ctx,
	}
	binding, bindingErr := toolcontext.ResolveComputerBinding(ctx, identity)
	// A nil task service makes any business execution impossible. The real
	// Source gate still runs first and must accept only a trusted binding.
	_, sourceErr := computertask.NewSource(nil).CallTool(ctx, identity, "computer_targets", map[string]any{})
	p.mu.Lock()
	p.calls++
	p.session, p.binding, p.bindingErr, p.sourceErr = session, binding, bindingErr, sourceErr
	p.mu.Unlock()
	return nil, nil
}

func scheduleWorkspaceMemoryFixture(t *testing.T) (directLifecycleFixture, *scheduleWorkspaceRejectNetwork) {
	t.Helper()
	provider := modelSelectionProviderRow(t, directLifecycleProviderID, string(models.ClientTypeOpenAICompletions), true)
	provider.Config = []byte(`{"api_key":"synthetic-fixture-only","base_url":"https://network-forbidden.invalid"}`)
	model := modelSelectionModelRow(t, directLifecycleModelID, "synthetic-model", provider.ID, models.ModelTypeChat, true)
	model.Config = []byte(`{"context_window":128000,"compatibilities":["tool-call"]}`)
	queries := &directLifecycleQueries{
		modelSelectionFakeQueries: modelSelectionFakeQueries{models: map[string]sqlc.Model{model.ModelID: model}, provider: provider},
		modelID:                   model.ID,
	}
	logger := slog.New(slog.DiscardHandler)
	reject := &scheduleWorkspaceRejectNetwork{}
	fixture := directLifecycleFixture{
		lifecycles: &synchronizedLifecycleStore{}, messages: &recordingMessageService{},
		runtime: &lifecycleTurnAdmitter{admission: lifecycleTestAdmission()},
	}
	fixture.service = &Service{
		modelsService: models.NewService(logger, queries), queries: queries,
		contextLifecycles: fixture.lifecycles, messageService: fixture.messages,
		settingsService: settings.NewService(logger, queries, nil, nil), sessionRuntime: fixture.runtime,
		streamHTTPClient: &http.Client{Transport: reject}, nonStreamingHTTPClient: &http.Client{Transport: reject},
		logger: logger,
		sessionService: sessionWorkdirSessionService{thread: sessionpkg.Thread{
			ID: lifecycleTestSessionID, BotID: lifecycleTestBotID, RuntimeType: sessionpkg.RuntimeModel,
			WorkdirID: "workdir-synthetic",
		}},
		workdirs: fakeSessionWorkdirResolver{resolved: workdir.Resolved{
			WorkdirID: "workdir-synthetic", TargetID: "pc-synthetic", Kind: workdir.TargetKindRemote, WorkDir: `C:\Synthetic\Work`,
		}},
		workspaceTargets: scheduleWorkspaceTargets{targetID: "pc-synthetic", kind: workdir.TargetKindRemote}, botPermissions: workspaceRequestPermission(true),
	}
	return fixture, reject
}

func triggerWorkspaceSchedule(service *Service) (schedule.TriggerResult, error) {
	return NewScheduleGateway(service).TriggerSchedule(context.Background(), lifecycleTestBotID, schedule.TriggerPayload{
		FireID: "synthetic-fire", ID: "synthetic-schedule", SessionID: lifecycleTestSessionID,
		Command: "synthetic scheduled workspace check", OwnerUserID: "synthetic-user",
	}, "")
}

func TestTriggerSchedulePreparesStoredWorkspace(t *testing.T) {
	for _, tc := range []struct{ name, targetID, kind string }{
		{"remote PC", "pc-synthetic", workdir.TargetKindRemote},
		{"native NAS workspace", workspace.WorkspaceTargetNative, workdir.TargetKindNative},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, network := scheduleWorkspaceMemoryFixture(t)
			fixture.service.workdirs = fakeSessionWorkdirResolver{resolved: workdir.Resolved{
				WorkdirID: "workdir-synthetic", TargetID: tc.targetID, Kind: tc.kind, WorkDir: "/synthetic/work",
			}}
			fixture.service.workspaceTargets = scheduleWorkspaceTargets{targetID: tc.targetID, kind: tc.kind}
			if tc.kind == workdir.TargetKindNative {
				fixture.service.botPermissions = nil
			}
			provider := configureTriggerLifecycleContextView(t, fixture, nil)
			probe := &scheduleWorkspaceBindingProbe{}
			fixture.service.agent.SetToolProviders([]tools.ToolProvider{probe})
			result, err := triggerWorkspaceSchedule(fixture.service)
			if err != nil || result.Status != "ok" {
				t.Fatalf("scheduled turn: status=%q error=%v", result.Status, err)
			}
			if result.Text != directLifecycleResponse || len(fixture.messages.persisted) != 2 {
				t.Fatal("scheduled response was not returned and persisted with its request")
			}
			for _, message := range fixture.messages.persisted {
				if message.SessionID != lifecycleTestSessionID {
					t.Fatal("scheduled output escaped the stored target session")
				}
			}
			probe.mu.Lock()
			defer probe.mu.Unlock()
			if probe.calls != 1 || provider.callCount() != 1 {
				t.Fatalf("native tool assembly=%d, synthetic provider calls=%d", probe.calls, provider.callCount())
			}
			want := toolcontext.ComputerBinding{
				BotID: lifecycleTestBotID, UserID: "synthetic-user", SessionID: lifecycleTestSessionID,
				TargetID: tc.targetID, RunID: lifecycleTestRunID,
			}
			if probe.bindingErr != nil || probe.binding != want {
				t.Fatalf("binding=%+v error=%v, want %+v", probe.binding, probe.bindingErr, want)
			}
			if probe.session.WorkspaceTargetID != tc.targetID || !errors.Is(probe.sourceErr, computertask.ErrDenied) {
				t.Fatalf("source must pass binding and stop at nil task service: target=%s error=%v", probe.session.WorkspaceTargetID, probe.sourceErr)
			}
			if network.calls.Load() != 0 {
				t.Fatal("unexpected network request")
			}
		})
	}
}

func TestTriggerScheduleRejectsUnavailableWorkspaceBeforeTools(t *testing.T) {
	offline := errors.New("synthetic PC offline")
	for _, tc := range []struct {
		name      string
		configure func(*Service)
	}{
		{"workspace permission revoked", func(s *Service) { s.botPermissions = workspaceRequestPermission(false) }},
		{"workdir resolution fails", func(s *Service) { s.workdirs = fakeSessionWorkdirResolver{err: offline} }},
		{"target offline", func(s *Service) { s.workspaceTargets = scheduleWorkspaceTargets{err: offline} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, network := scheduleWorkspaceMemoryFixture(t)
			tc.configure(fixture.service)
			provider := configureTriggerLifecycleContextView(t, fixture, nil)
			probe := &scheduleWorkspaceBindingProbe{}
			fixture.service.agent.SetToolProviders([]tools.ToolProvider{probe})
			result, err := triggerWorkspaceSchedule(fixture.service)
			if err == nil || result.Status != "" {
				t.Fatalf("unexpected success: result=%+v error=%v", result, err)
			}
			probe.mu.Lock()
			defer probe.mu.Unlock()
			if probe.calls != 0 || provider.callCount() != 0 || network.calls.Load() != 0 {
				t.Fatalf("rejected turn reached tools/provider/network: %d/%d/%d", probe.calls, provider.callCount(), network.calls.Load())
			}
		})
	}
}
