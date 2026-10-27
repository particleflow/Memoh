package botdelegation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/workdir"
)

type authorityFixture struct {
	sessions map[string]thread.Thread
	teams    map[string]string
	denied   map[string]bool
	mounts   map[string]dbstore.BotRemoteRuntimeBindingRecord
	targets  map[string]agenvo.Target
	assets   map[string]bool
	workdirs map[string]workdir.Workdir
	active   bool
}

func fixture() (*Authority, *authorityFixture, Identity, DevicePair) {
	f := &authorityFixture{
		sessions: map[string]thread.Thread{
			"root":               {ID: "root", TeamID: "team", BotID: "manager", CreatedByUserID: "user", Type: thread.TypeChat, Metadata: map[string]any{"workspace_target_id": "native"}},
			"child":              {ID: "child", TeamID: "team", BotID: "manager", CreatedByUserID: "user", Type: thread.TypeSubagent, ParentThreadID: "root"},
			"specialist-session": {ID: "specialist-session", TeamID: "team", BotID: "specialist", CreatedByUserID: "user", Type: thread.TypeChat},
		},
		teams: map[string]string{"manager": "team", "specialist": "team"}, denied: map[string]bool{},
		mounts: map[string]dbstore.BotRemoteRuntimeBindingRecord{}, targets: map[string]agenvo.Target{},
		assets: map[string]bool{}, workdirs: map[string]workdir.Workdir{}, active: true,
	}
	target := agenvo.Target{ConnectionID: "20000000-0000-4000-8000-000000000001", DeviceID: "synthetic-device", InstanceID: "synthetic-instance", Session: "synthetic-session", WorkDir: `C:\synthetic`, BashPath: `C:\synthetic\bin\bash.exe`}
	for _, side := range []struct{ bot, mount string }{{"manager", "manager-pc-a"}, {"specialist", "specialist-pc-a"}} {
		f.mounts[side.bot+"/"+side.mount] = dbstore.BotRemoteRuntimeBindingRecord{ID: side.mount, BotID: side.bot, RuntimeID: "pc-a", RuntimeUserID: "owner", BotOwnerUserID: "owner", RuntimeProvider: agenvo.Provider}
		f.targets[side.bot+"/"+side.mount] = target
	}
	a := &Authority{Permissions: f, Sessions: sessionFixture{f}, Workdirs: workdirFixture{f}, Mounts: f, Assets: f}
	b := Identity{TeamID: "team", UserID: "user", SourceBotID: "manager", SourceSessionID: "child", SourceRootID: "root", TargetBotID: "specialist", TargetSessionID: "specialist-session"}
	p := DevicePair{SourceMountID: "manager-pc-a", TargetMountID: "specialist-pc-a", RuntimeID: "pc-a", Fingerprint: agenvo.TargetFingerprint(target)}
	return a, f, b, p
}

func (f *authorityFixture) ValidateSession(context.Context, string) error {
	if !f.active {
		return errors.New("SECRET session diagnostic")
	}
	return nil
}

func (f *authorityFixture) BotTeam(_ context.Context, bot string) (string, error) {
	return f.teams[bot], nil
}

func (f *authorityFixture) Allowed(_ context.Context, user, bot, permission string) (bool, error) {
	return user == "user" && f.teams[bot] != "" && !f.denied[bot+"/"+permission], nil
}

type sessionFixture struct{ *authorityFixture }

func (f sessionFixture) Get(_ context.Context, id string) (thread.Thread, error) {
	s, ok := f.sessions[id]
	if !ok {
		return thread.Thread{}, db.ErrNotFound
	}
	return s, nil
}

type workdirFixture struct{ *authorityFixture }

func (f workdirFixture) Get(_ context.Context, bot, id string) (workdir.Workdir, error) {
	w, ok := f.workdirs[bot+"/"+id]
	if !ok {
		return workdir.Workdir{}, db.ErrNotFound
	}
	return w, nil
}

func (f *authorityFixture) GetMount(_ context.Context, bot, id string) (dbstore.BotRemoteRuntimeBindingRecord, error) {
	m, ok := f.mounts[bot+"/"+id]
	if !ok {
		return dbstore.BotRemoteRuntimeBindingRecord{}, db.ErrNotFound
	}
	if len(m.RuntimeProviderConfig) == 0 {
		m.RuntimeProviderConfig, _ = json.Marshal(f.targets[bot+"/"+id])
	}
	return m, nil
}

func (*authorityFixture) OwnedNativeHerdrRuntime(context.Context, string, string) (dbstore.UserRuntimeRecord, error) {
	return dbstore.UserRuntimeRecord{}, db.ErrNotFound
}

func (*authorityFixture) GetPrimaryMount(context.Context, string) (dbstore.BotRemoteRuntimeBindingRecord, error) {
	return dbstore.BotRemoteRuntimeBindingRecord{}, db.ErrNotFound
}

func (*authorityFixture) OwnedHTTPMCPConnectionBot(context.Context, string, string) (string, error) {
	return "manager", nil
}

func (f *authorityFixture) SessionHasAsset(_ context.Context, bot, session, hash string) (bool, error) {
	return f.assets[bot+"/"+session+"/"+hash], nil
}

func (f *authorityFixture) ResultHasAsset(_ context.Context, bot, user, root, hash string) (bool, error) {
	for id, s := range f.sessions {
		if !f.assets[bot+"/"+id+"/"+hash] {
			continue
		}
		seen := map[string]bool{}
		for s.ID != "" && !seen[s.ID] && s.BotID == bot && s.CreatedByUserID == user && s.TeamID == f.teams[bot] {
			if s.ID == root {
				return true, nil
			}
			seen[s.ID] = true
			if s.Type != thread.TypeSubagent {
				break
			}
			s = f.sessions[s.ParentThreadID]
		}
	}
	return false, nil
}

func TestAuthorizedSpecialistHandoffKeepsDistinctOwnership(t *testing.T) {
	a, _, b, pair := fixture()
	got, selected, err := a.Bind(t.Context(), b.UserID, b.SourceBotID, b.SourceSessionID, b.TargetBotID)
	if err != nil || selected != "native" || got.SourceRootID != "root" || got.TargetBotID != "specialist" || got.TargetSessionID != "" {
		t.Fatalf("binding = %+v, %q, %v", got, selected, err)
	}
	if err := a.AuthorizeDevice(t.Context(), b, pair, bots.PermissionWorkspaceExec); err != nil {
		t.Fatalf("same runtime through distinct authorized mounts: %v", err)
	}
}

func TestHandoffRechecksEveryAncestorBothBotsAndActualUser(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*authorityFixture, *Identity)
	}{
		{"other user", func(_ *authorityFixture, b *Identity) { b.UserID = "other" }},
		{"parent owner", func(f *authorityFixture, _ *Identity) {
			s := f.sessions["root"]
			s.CreatedByUserID = "other"
			f.sessions["root"] = s
		}},
		{"parent bot", func(f *authorityFixture, _ *Identity) {
			s := f.sessions["root"]
			s.BotID = "other"
			f.sessions["root"] = s
		}},
		{"parent team", func(f *authorityFixture, _ *Identity) {
			s := f.sessions["root"]
			s.TeamID = "other"
			f.sessions["root"] = s
		}},
		{"target team", func(f *authorityFixture, _ *Identity) { f.teams["specialist"] = "other" }},
		{"source grant revoked", func(f *authorityFixture, _ *Identity) { f.denied["manager/chat"] = true }},
		{"target grant revoked", func(f *authorityFixture, _ *Identity) { f.denied["specialist/chat"] = true }},
		{"user revoked", func(f *authorityFixture, _ *Identity) { f.active = false }},
		{"parent cycle", func(f *authorityFixture, _ *Identity) {
			s := f.sessions["root"]
			s.Type = thread.TypeSubagent
			s.ParentThreadID = "child"
			f.sessions["root"] = s
		}},
		{"forged root", func(_ *authorityFixture, b *Identity) { b.SourceRootID = "child" }},
		{"target owner", func(f *authorityFixture, _ *Identity) {
			s := f.sessions["specialist-session"]
			s.CreatedByUserID = "other"
			f.sessions[s.ID] = s
		}},
		{"cross bot parent", func(f *authorityFixture, _ *Identity) {
			s := f.sessions["specialist-session"]
			s.ParentThreadID = "root"
			f.sessions[s.ID] = s
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, f, b, _ := fixture()
			if _, err := a.Authorize(t.Context(), b); err != nil {
				t.Fatal(err)
			}
			tc.change(f, &b)
			if _, err := a.Authorize(t.Context(), b); !errors.Is(err, ErrDenied) || strings.Contains(err.Error(), "SECRET") {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestHandoffComputerIntersectionAndRevocation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*authorityFixture, *DevicePair)
	}{
		{"selected other PC", func(f *authorityFixture, _ *DevicePair) {
			s := f.sessions["root"]
			s.Metadata = map[string]any{"workspace_target_id": "pc-b"}
			f.sessions["root"] = s
			f.targets["manager/pc-b"] = agenvo.Target{}
		}},
		{"source revoked", func(f *authorityFixture, _ *DevicePair) { delete(f.mounts, "manager/manager-pc-a") }},
		{"target revoked", func(f *authorityFixture, _ *DevicePair) { delete(f.mounts, "specialist/specialist-pc-a") }},
		{"runtime revoked", func(f *authorityFixture, _ *DevicePair) {
			m := f.mounts["specialist/specialist-pc-a"]
			m.RuntimeRevoked = true
			f.mounts["specialist/specialist-pc-a"] = m
		}},
		{"owner changed", func(f *authorityFixture, _ *DevicePair) {
			m := f.mounts["specialist/specialist-pc-a"]
			m.RuntimeUserID = "other"
			f.mounts["specialist/specialist-pc-a"] = m
		}},
		{"runtime changed", func(f *authorityFixture, _ *DevicePair) {
			m := f.mounts["specialist/specialist-pc-a"]
			m.RuntimeID = "pc-b"
			f.mounts["specialist/specialist-pc-a"] = m
		}},
		{"registration changed", func(f *authorityFixture, _ *DevicePair) {
			v := f.targets["specialist/specialist-pc-a"]
			v.InstanceID = "other"
			f.targets["specialist/specialist-pc-a"] = v
		}},
		{"source exec revoked", func(f *authorityFixture, _ *DevicePair) { f.denied["manager/workspace_exec"] = true }},
		{"target exec revoked", func(f *authorityFixture, _ *DevicePair) { f.denied["specialist/workspace_exec"] = true }},
		{"forged pair", func(_ *authorityFixture, p *DevicePair) { p.RuntimeID = "pc-b" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, f, b, p := fixture()
			if err := a.AuthorizeDevice(t.Context(), b, p, bots.PermissionWorkspaceExec); err != nil {
				t.Fatal(err)
			}
			tc.change(f, &p)
			if err := a.AuthorizeDevice(t.Context(), b, p, bots.PermissionWorkspaceExec); !errors.Is(err, ErrDenied) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestPCSelectionCannotBroadenThroughChildMetadata(t *testing.T) {
	a, f, b, p := fixture()
	s := f.sessions["root"]
	s.Metadata = map[string]any{"workspace_target_id": "manager-pc-a"}
	f.sessions["root"] = s
	if err := a.AuthorizeDevice(t.Context(), b, p, bots.PermissionWorkspaceExec); err != nil {
		t.Fatal(err)
	}
	s = f.sessions["child"]
	s.Metadata = map[string]any{"workspace_target_id": "native"}
	f.sessions["child"] = s
	if _, err := a.Authorize(t.Context(), b); !errors.Is(err, ErrDenied) {
		t.Fatalf("got %v", err)
	}
}

func TestSingleAttachmentMustBeLinkedToTheCorrectConversationAndRemainAuthorized(t *testing.T) {
	a, f, b, _ := fixture()
	hash := strings.Repeat("a", 64)
	f.assets["manager/root/"+hash] = true
	if err := a.AuthorizeAsset(t.Context(), b, hash, false); err != nil {
		t.Fatal(err)
	}
	if err := a.AuthorizeAsset(t.Context(), b, hash, true); !errors.Is(err, ErrDenied) {
		t.Fatalf("input is not a result: %v", err)
	}
	f.assets["specialist/specialist-session/"+hash] = true
	if err := a.AuthorizeAsset(t.Context(), b, hash, true); err != nil {
		t.Fatal(err)
	}
	delete(f.assets, "manager/root/"+hash)
	f.assets["manager/other-session/"+hash] = true
	if err := a.AuthorizeAsset(t.Context(), b, hash, false); !errors.Is(err, ErrDenied) {
		t.Fatalf("other session: %v", err)
	}
	f.denied["manager/chat"] = true
	if err := a.AuthorizeAsset(t.Context(), b, hash, true); !errors.Is(err, ErrDenied) {
		t.Fatalf("result after revocation: %v", err)
	}
}

func TestPCTextAndStoredMediaDoNotRequireExecutionPermission(t *testing.T) {
	a, f, b, p := fixture()
	s := f.sessions["root"]
	s.Metadata = map[string]any{"workspace_target_id": p.SourceMountID}
	f.sessions["root"] = s
	f.denied["manager/workspace_exec"] = true
	f.denied["specialist/workspace_exec"] = true
	if _, err := a.Authorize(t.Context(), b); err != nil {
		t.Fatalf("text authorization unexpectedly requires exec: %v", err)
	}
	hash := strings.Repeat("a", 64)
	f.assets["manager/root/"+hash] = true
	if err := a.AuthorizeAsset(t.Context(), b, hash, false); err != nil {
		t.Fatalf("stored media unexpectedly requires exec: %v", err)
	}
	if err := a.AuthorizeDevice(t.Context(), b, p, bots.PermissionWorkspaceRead); err != nil {
		t.Fatalf("read unexpectedly requires exec: %v", err)
	}
	if err := a.AuthorizeDevice(t.Context(), b, p, bots.PermissionWorkspaceExec); !errors.Is(err, ErrDenied) {
		t.Fatalf("actual exec was not rejected: %v", err)
	}
}

func TestResultCanBelongToOwnedSpecialistDescendant(t *testing.T) {
	a, f, b, _ := fixture()
	hash := strings.Repeat("c", 64)
	f.sessions["specialist-child"] = thread.Thread{ID: "specialist-child", TeamID: "team", BotID: "specialist", CreatedByUserID: "user", Type: thread.TypeSubagent, ParentThreadID: b.TargetSessionID}
	f.assets["specialist/specialist-child/"+hash] = true
	if err := a.AuthorizeAsset(t.Context(), b, hash, true); err != nil {
		t.Fatalf("owned child result: %v", err)
	}
	s := f.sessions["specialist-child"]
	s.CreatedByUserID = "other"
	f.sessions[s.ID] = s
	if err := a.AuthorizeAsset(t.Context(), b, hash, true); !errors.Is(err, ErrDenied) {
		t.Fatalf("foreign child result: %v", err)
	}
}
