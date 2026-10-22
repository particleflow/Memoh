package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/config"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/mcp"
	"github.com/felinics/memoh/internal/toolcontext"
	"github.com/felinics/memoh/internal/workspace"
)

// The real source, authorizer, workspace resolver and Agenvo probe share this
// synthetic registry. No gateway, token or remote computer is contacted.
type computerPreflightStore struct {
	dbstore.AgenvoRuntimeStore
	dbstore.BotRemoteRuntimeBindingStore
	dbstore.ComputerOperationStore
	mount     dbstore.BotRemoteRuntimeBindingRecord
	operation *dbstore.ComputerOperationRecord
	claims    int
	saves     int
}

func (s *computerPreflightStore) GetMount(_ context.Context, bot, target string) (dbstore.BotRemoteRuntimeBindingRecord, error) {
	if bot != s.mount.BotID || target != s.mount.ID {
		return dbstore.BotRemoteRuntimeBindingRecord{}, computertask.ErrDenied
	}
	return s.mount, nil
}

func (s *computerPreflightStore) ListMounts(_ context.Context, bot string) ([]dbstore.BotRemoteRuntimeBindingRecord, error) {
	if bot != s.mount.BotID {
		return nil, computertask.ErrDenied
	}
	return []dbstore.BotRemoteRuntimeBindingRecord{s.mount}, nil
}

func (s *computerPreflightStore) OwnedHTTPMCPConnectionBot(_ context.Context, owner, connection string) (string, error) {
	if owner != s.mount.RuntimeUserID || connection != "55555555-5555-4555-8555-555555555555" {
		return "", agenvo.ErrForbidden
	}
	return s.mount.BotID, nil
}

func (s *computerPreflightStore) ClaimComputerOperation(_ context.Context, r dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, bool, error) {
	s.claims++
	if s.operation != nil {
		return *s.operation, false, nil
	}
	return r, false, errors.New("unexpected claim after failed preflight")
}

func (s *computerPreflightStore) SaveComputerOperation(_ context.Context, r dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
	s.saves++
	r.Revision++
	s.operation = &r
	return r, nil
}

func TestComputerTaskPreflightFailureIsNotConversationDenial(t *testing.T) {
	for _, tc := range []struct {
		name, target, denied       string
		missing, mismatch, otherPC bool
		unknown                    bool
		want                       error
		probes                     int
	}{
		{name: "native_explicit", target: "44444444-4444-4444-8444-444444444444", want: computertask.ErrPreflightUnavailable, probes: 1},
		{name: "native_inferred", want: computertask.ErrPreflightUnavailable, probes: 1},
		{name: "unknown_native_explicit", target: "44444444-4444-4444-8444-444444444444", unknown: true, want: computertask.ErrPreflightUnavailable, probes: 1},
		{name: "unknown_native_inferred", unknown: true, want: computertask.ErrPreflightUnavailable, probes: 1},
		{name: "missing_binding", missing: true, want: toolcontext.ErrComputerBinding},
		{name: "wrong_session", mismatch: true, want: toolcontext.ErrComputerBinding},
		{name: "other_pc_scope", otherPC: true, target: "44444444-4444-4444-8444-444444444444", want: computertask.ErrDenied},
		{name: "read_denied", denied: "workspace_read", want: computertask.ErrDenied},
		{name: "write_denied", denied: "workspace_write", want: computertask.ErrDenied},
		{name: "exec_denied", denied: "workspace_exec", want: computertask.ErrDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const bot = "11111111-1111-4111-8111-111111111111"
			const owner = "33333333-3333-4333-8333-333333333333"
			target := agenvo.Target{ConnectionID: "55555555-5555-4555-8555-555555555555", DeviceID: "synthetic-device", InstanceID: "synthetic-herdr", Session: "synthetic", WorkDir: `C:\Synthetic`, BashPath: `C:\Synthetic\Git\bin\bash.exe`, Agent: agenvo.AgentProfile{ExecutionMode: "pi-json-v1", Provider: "synthetic", Model: "test", ConfigDir: `C:\Synthetic\Config`}}
			raw, err := json.Marshal(target)
			if err != nil {
				t.Fatal(err)
			}
			store := &computerPreflightStore{mount: dbstore.BotRemoteRuntimeBindingRecord{ID: "44444444-4444-4444-8444-444444444444", BotID: bot, RuntimeUserID: owner, BotOwnerUserID: owner, RuntimeProvider: agenvo.Provider, RuntimeProviderConfig: raw}}
			if tc.unknown {
				store.operation = &dbstore.ComputerOperationRecord{
					ID: "66666666-6666-4666-8666-666666666666", BotID: bot, UserID: owner,
					SessionID: "root-session", RunID: "previous-run", TargetID: store.mount.ID,
					State: "unknown", Revision: 7, CancelRequested: true,
					Details: dbstore.JSON(`{"kind":"herdr_pi_task","phase":"dispatch_unknown","workspace_id":"synthetic-workspace","pane_id":"synthetic-pane"}`),
				}
			}
			before, err := json.Marshal(store.operation)
			if err != nil {
				t.Fatal(err)
			}
			probes := 0
			permissions := computerPermissions{denied: tc.denied}
			relay := agenvo.NewService(store, permissions, func(_ context.Context, _, _, tool string, args map[string]any) (json.RawMessage, error) {
				probes++
				code, _ := args["code"].(string)
				if tool != "execute" || !strings.Contains(code, `"workspace.list"`) {
					t.Fatal("preflight failure dispatched a business operation")
				}
				return nil, errors.New("synthetic OAuth refresh failure: SECRET")
			})
			remote := workspace.NewRemoteWorkspaceService(store, nil)
			remote.SetAgenvoService(relay)
			manager := workspace.NewManager(slog.New(slog.DiscardHandler), nil, nil, config.WorkspaceConfig{}, "", nil)
			manager.SetRemoteWorkspaceService(remote)
			authorize := computerTaskAuthorizer(permissions, manager.ResolveWorkspaceTarget, nil)
			source := computertask.NewSource(computertask.NewService(store, store, relay, nil, authorize))
			ctx := t.Context()
			selection := "native"
			if tc.otherPC {
				selection = "44444444-4444-4444-8444-444444444445"
			}
			if !tc.missing {
				ctx, err = toolcontext.WithComputerBinding(ctx, toolcontext.ComputerBinding{BotID: bot, UserID: owner, SessionID: "root-session", RunID: "new-run", TargetID: selection})
				if err != nil {
					t.Fatal(err)
				}
			}
			session := mcp.ToolSessionContext{BotID: bot, UserID: owner, SessionID: "root-session", RunID: "new-run", RequireActiveRun: true, RuntimeActive: true, RunContext: ctx}
			if tc.mismatch {
				session.SessionID = "other-session"
			}
			args := map[string]any{"kind": "herdr_pi_task", "task": "Compute the synthetic value 6 * 7.", "work_dir": "."}
			if tc.target != "" {
				args["target_id"] = tc.target
			}
			result, err := source.CallTool(ctx, session, "computer_task_submit", args)
			if errors.Is(tc.want, computertask.ErrPreflightUnavailable) {
				if err != nil || result["isError"] != true {
					t.Fatalf("preflight wire result=%v err=%v", result, err)
				}
				data, ok := result["structuredContent"].(map[string]any)
				if !ok || data["code"] != "computer.connection_unavailable" || data["phase"] != "preflight" || data["operation_created"] != false || data["business_dispatched"] != false {
					t.Fatalf("missing machine-readable preflight facts: %v", data)
				}
			} else if !errors.Is(err, tc.want) || result != nil {
				t.Fatalf("result=%v err=%v want=%v", result, err, tc.want)
			}
			wire, marshalErr := json.Marshal(result)
			after, recordErr := json.Marshal(store.operation)
			if recordErr != nil || !bytes.Equal(before, after) || store.saves != 0 {
				t.Fatal("failed preflight changed an existing operation or its revision")
			}
			if marshalErr != nil || strings.Contains(string(wire), "SECRET") || (err != nil && strings.Contains(err.Error(), "SECRET")) || store.claims != 0 || probes != tc.probes {
				t.Fatalf("error leaked or effect reached: claims=%d probes=%d", store.claims, probes)
			}
		})
	}
}
