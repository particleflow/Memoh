package agenvo

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/toolcontext"
)

type serviceStore struct {
	record dbstore.BotRemoteRuntimeBindingRecord
}

func TestServiceComputerScopeRejectsAuthorizedOtherPCBeforeRelay(t *testing.T) {
	s, _, f := serviceFixture(t)
	ctx := toolcontext.WithComputerScope(t.Context(), "mount-b")
	result, err := s.Execute(ctx, request()) // mount-a is a valid Bot grant
	if !errors.Is(err, toolcontext.ErrComputerScope) || result.State != "not_started" || len(f.calls) != 0 {
		t.Fatalf("cross-PC execution: state=%s err=%v calls=%v", result.State, err, f.calls)
	}
	for _, selected := range []string{"native", "mount-a"} {
		s, _, f := serviceFixture(t)
		result, err := s.Execute(toolcontext.WithComputerScope(t.Context(), selected), request())
		if err != nil || result.State != "completed" || f.runs != 1 {
			t.Fatalf("authorized selected PC: selected=%s state=%s err=%v runs=%d", selected, result.State, err, f.runs)
		}
	}
}

func (s *serviceStore) GetMount(_ context.Context, bot, target string) (dbstore.BotRemoteRuntimeBindingRecord, error) {
	if bot != s.record.BotID || target != s.record.ID {
		return dbstore.BotRemoteRuntimeBindingRecord{}, errors.New("not found")
	}
	return s.record, nil
}

type permissionFunc func(context.Context, string, string, string) (bool, error)

func (f permissionFunc) HasBotPermission(ctx context.Context, b, u, p string) (bool, error) {
	return f(ctx, b, u, p)
}

func serviceFixture(t *testing.T) (*Service, *serviceStore, *fixture) {
	f, _ := newFixture(t)
	f.target.ConnectionID = "11111111-1111-4111-8111-111111111111"
	f.target.DeviceID, f.target.NativeRuntimeID = f.target.ConnectionID, f.target.ConnectionID
	raw := serviceFixtureConfig(f.target)
	f.target, _ = DecodeNativeTarget(f.target.NativeRuntimeID, raw)
	store := &serviceStore{record: dbstore.BotRemoteRuntimeBindingRecord{ID: "mount-a", BotID: "bot-a", RuntimeID: f.target.NativeRuntimeID, RuntimeUserID: "owner", BotOwnerUserID: "owner", RuntimeProvider: "memoh", RuntimeProviderConfig: raw}}
	permissions := permissionFunc(func(_ context.Context, b, u, p string) (bool, error) {
		return b == "bot-a" && u == "user" && p == "workspace_exec", nil
	})
	service := NewService(store, permissions, f.call)

	return service, store, f
}

func TestServiceRejectsAuthorizationFailuresBeforeRelay(t *testing.T) {
	for _, test := range []string{"no_user", "no_permission", "other_bot", "other_mount", "revoked", "owner_changed", "wrong_provider", "invalid_config"} {
		t.Run(test, func(t *testing.T) {
			s, store, f := serviceFixture(t)
			req := request()
			switch test {
			case "no_user":
				req.Scope.UserID = ""
			case "no_permission":
				req.Scope.UserID = "other-user"
			case "other_bot":
				req.Scope.BotID = "bot-b"
			case "other_mount":
				req.Scope.TargetID = "mount-b"
			case "revoked":
				store.record.RuntimeRevoked = true
			case "owner_changed":
				store.record.BotOwnerUserID = "new-owner"
			case "wrong_provider":
				store.record.RuntimeProvider = "unsupported"
			case "invalid_config":
				store.record.RuntimeProviderConfig = []byte(`{}`)
			}
			result, err := s.Execute(t.Context(), req)
			if err == nil || result.State != "not_started" || len(f.calls) != 0 {
				t.Fatalf("result=%+v err=%v calls=%v", result, err, f.calls)
			}
		})
	}
}

func TestServiceRevocationAfterSubmissionBlocksReads(t *testing.T) {
	s, store, f := serviceFixture(t)
	f.afterSubmit = func() { store.record.RuntimeRevoked = true }
	r, err := s.Execute(t.Context(), request())
	if !errors.Is(err, ErrForbidden) || r.State != "unknown" || r.ExitCode != nil || !r.CleanupRequired || f.runs != 1 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	for _, call := range f.calls {
		if call == "pane.read" || call == "workspace.close" {
			t.Fatal("used revoked credentials")
		}
	}
}

func serviceFixtureConfig(target Target) []byte {
	raw, _ := json.Marshal(map[string]any{"herdr": NativeHerdrProfile{InstanceID: target.InstanceID, Session: target.Session, WorkDir: target.WorkDir, BashPath: target.BashPath, HerdrCLI: `C:\Synthetic\herdr.exe`, HerdrConfigRoot: `C:\Synthetic\herdr`, Agent: target.Agent}})
	return raw
}

func TestOperationHistoryPermissionDoesNotResolveRegistration(t *testing.T) {
	s, store, f := serviceFixture(t)
	store.record.RuntimeRevoked = true
	scope := request().Scope
	if err := s.AuthorizeOperationHistory(t.Context(), scope); err != nil {
		t.Fatal(err)
	}
	store.record = dbstore.BotRemoteRuntimeBindingRecord{}
	if err := s.AuthorizeOperationHistory(t.Context(), scope); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveTarget(t.Context(), scope); err == nil {
		t.Fatal("missing registration allowed execution")
	}
	if err := s.AuthorizeOperationHistory(toolcontext.WithComputerScope(t.Context(), "other"), scope); err == nil {
		t.Fatal("cross PC history allowed")
	}
	scope.UserID = "other"
	if err := s.AuthorizeOperationHistory(t.Context(), scope); err == nil {
		t.Fatal("permission denied history allowed")
	}
	if len(f.calls) != 0 {
		t.Fatal("history invoked transport")
	}
}
