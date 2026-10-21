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
	record        dbstore.BotRemoteRuntimeBindingRecord
	sourceAllowed bool
	created       int
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

func (s *serviceStore) OwnedHTTPMCPConnectionBot(context.Context, string, string) (string, error) {
	if !s.sourceAllowed {
		return "", errors.New("private source denied")
	}
	return "source-bot", nil
}

func (s *serviceStore) GetMount(_ context.Context, bot, target string) (dbstore.BotRemoteRuntimeBindingRecord, error) {
	if bot != s.record.BotID || target != s.record.ID {
		return dbstore.BotRemoteRuntimeBindingRecord{}, errors.New("not found")
	}
	return s.record, nil
}

func (s *serviceStore) CreateAgenvoRuntime(_ context.Context, owner, name string, config dbstore.JSON) (dbstore.UserRuntimeRecord, error) {
	s.created++
	return dbstore.UserRuntimeRecord{ID: "registered", UserID: owner, Name: name, Provider: Provider, ProviderConfig: config}, nil
}

type permissionFunc func(context.Context, string, string, string) (bool, error)

func (f permissionFunc) HasBotPermission(ctx context.Context, b, u, p string) (bool, error) {
	return f(ctx, b, u, p)
}

func serviceFixture(t *testing.T) (*Service, *serviceStore, *fixture) {
	f, _ := newFixture(t)
	f.target.ConnectionID = "11111111-1111-4111-8111-111111111111"
	raw, _ := json.Marshal(f.target)
	store := &serviceStore{sourceAllowed: true, record: dbstore.BotRemoteRuntimeBindingRecord{ID: "mount-a", BotID: "bot-a", RuntimeUserID: "owner", BotOwnerUserID: "owner", RuntimeProvider: Provider, RuntimeProviderConfig: raw}}
	permissions := permissionFunc(func(_ context.Context, b, u, p string) (bool, error) {
		return b == "bot-a" && u == "user" && p == "workspace_exec", nil
	})
	service := NewService(store, permissions, func(ctx context.Context, owner, id, name string, args map[string]any) (json.RawMessage, error) {
		if owner != "owner" {
			t.Fatal("incorrect credential owner")
		}
		return f.call(ctx, id, name, args)
	})
	return service, store, f
}

func TestServiceRegistrationProbesAndDoesNotCreateGrants(t *testing.T) {
	s, store, f := serviceFixture(t)
	record, err := s.Register(t.Context(), "owner", "Windows", f.target)
	if err != nil || store.created != 1 || record.Provider != Provider || len(f.calls) != 1 || f.calls[0] != "workspace.list" {
		t.Fatalf("record=%+v err=%v calls=%v", record, err, f.calls)
	}
}

func TestServiceRejectsAuthorizationFailuresBeforeRelay(t *testing.T) {
	for _, test := range []string{"no_user", "no_permission", "other_bot", "other_mount", "revoked", "owner_changed", "wrong_provider", "source_revoked", "invalid_config"} {
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
				store.record.RuntimeProvider = "memoh"
			case "source_revoked":
				store.sourceAllowed = false
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
	f.afterSubmit = func() { store.sourceAllowed = false }
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

func TestServiceDoesNotRegisterForeignOrOfflineSource(t *testing.T) {
	s, store, f := serviceFixture(t)
	store.sourceAllowed = false
	if _, err := s.Register(t.Context(), "owner", "Windows", f.target); !errors.Is(err, ErrForbidden) || store.created != 0 || len(f.calls) != 0 {
		t.Fatalf("err=%v created=%d calls=%v", err, store.created, f.calls)
	}
	store.sourceAllowed = true
	s.call = func(context.Context, string, string, string, map[string]any) (json.RawMessage, error) {
		return nil, errors.New("private offline")
	}
	if _, err := s.Register(t.Context(), "owner", "Windows", f.target); err == nil || store.created != 0 {
		t.Fatalf("err=%v created=%d", err, store.created)
	}
}

func TestServiceRequiresExplicitWindowsBashExecutable(t *testing.T) {
	for _, path := range []string{"", "bash", `C:\Windows\System32\wsl.exe`, "/usr/bin/bash", `\\server\share\bash.exe`} {
		s, store, f := serviceFixture(t)
		target := f.target
		target.BashPath = path
		if _, err := s.Register(t.Context(), "owner", "Windows", target); !errors.Is(err, ErrInvalidTarget) || store.created != 0 || len(f.calls) != 0 {
			t.Fatalf("unqualified or unsupported shell accepted: %q err=%v", path, err)
		}
	}
}

func TestDiscoveryFiltersPrivateMetadataAndNonHerdrInstances(t *testing.T) {
	s, _, _ := serviceFixture(t)
	s.call = func(context.Context, string, string, string, map[string]any) (json.RawMessage, error) {
		return mcpValue(map[string]any{"execution": "accepted", "result": map[string]any{"items": []map[string]any{{"kind": "herdr", "deviceId": "d", "instanceId": "i", "deviceLabel": "PC", "label": "Herdr", "online": true, "available": true, "context": "PRIVATE", "scope": "PRIVATE"}, {"kind": "codex", "context": "PRIVATE"}}}}), nil
	}
	items, err := s.Discover(t.Context(), "owner", "connection")
	if err != nil || len(items) != 1 || !items[0].Online || items[0].DeviceID != "d" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}
