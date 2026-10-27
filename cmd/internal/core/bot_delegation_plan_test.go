package core

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

const (
	planSourceBot   = "11111111-1111-4111-8111-111111111111"
	planTargetBot   = "22222222-2222-4222-8222-222222222222"
	planRuntime     = "33333333-3333-4333-8333-333333333333"
	planSourceMount = "44444444-4444-4444-8444-444444444444"
	planTargetMount = "55555555-5555-4555-8555-555555555555"
)

type delegationPlanFixture struct {
	mounts        map[string]dbstore.BotRemoteRuntimeBindingRecord
	session       thread.Thread
	registration  dbstore.UserRuntimeRecord
	missingTarget bool
	denied        bool
	lookups       int
}

func newDelegationPlanFixture(t *testing.T, selected string) (*delegationPlanFixture, *botdelegation.Authority, botdelegation.Identity) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"herdr": agenvo.NativeHerdrProfile{InstanceID: "synthetic-herdr", Session: "default", WorkDir: `C:\Synthetic`, BashPath: `C:\Git\bin\bash.exe`, HerdrCLI: `C:\Synthetic\herdr.exe`, HerdrConfigRoot: `C:\Synthetic\config\herdr`}})
	if err != nil {
		t.Fatal(err)
	}
	f := &delegationPlanFixture{mounts: map[string]dbstore.BotRemoteRuntimeBindingRecord{}, session: thread.Thread{ID: "source-session", BotID: planSourceBot, TeamID: "team", CreatedByUserID: "user", Type: thread.TypeChat, Metadata: map[string]any{"workspace_target_id": selected}}, registration: dbstore.UserRuntimeRecord{ID: planRuntime, UserID: "owner", Provider: "memoh", ActivatedAt: time.Now(), ProviderConfig: raw}}
	for bot, mount := range map[string]string{planSourceBot: planSourceMount, planTargetBot: planTargetMount} {
		f.mounts[bot+"/"+mount] = dbstore.BotRemoteRuntimeBindingRecord{ID: mount, BotID: bot, RuntimeID: planRuntime, RuntimeUserID: "owner", BotOwnerUserID: "owner", RuntimeProvider: "memoh", RuntimeProviderConfig: raw}
	}
	a := &botdelegation.Authority{Permissions: f, Sessions: f, Mounts: f}
	b := botdelegation.Identity{SourceSelection: selected, TeamID: "team", UserID: "user", SourceBotID: planSourceBot, SourceSessionID: f.session.ID, SourceRootID: f.session.ID, TargetBotID: planTargetBot}
	return f, a, b
}
func (*delegationPlanFixture) ValidateSession(context.Context, string) error   { return nil }
func (*delegationPlanFixture) BotTeam(context.Context, string) (string, error) { return "team", nil }
func (f *delegationPlanFixture) Allowed(_ context.Context, user, bot string, _ string) (bool, error) {
	return !f.denied && user == "user" && (bot == planSourceBot || bot == planTargetBot), nil
}

func (f *delegationPlanFixture) Get(_ context.Context, id string) (thread.Thread, error) {
	if id == f.session.ID {
		return f.session, nil
	}
	return thread.Thread{}, db.ErrNotFound
}

func (f *delegationPlanFixture) GetMount(_ context.Context, bot, mount string) (dbstore.BotRemoteRuntimeBindingRecord, error) {
	m, ok := f.mounts[bot+"/"+mount]
	if !ok {
		return m, db.ErrNotFound
	}
	return m, nil
}

func (*delegationPlanFixture) GetPrimaryMount(context.Context, string) (dbstore.BotRemoteRuntimeBindingRecord, error) {
	return dbstore.BotRemoteRuntimeBindingRecord{}, db.ErrNotFound
}

func (f *delegationPlanFixture) OwnedNativeHerdrRuntime(_ context.Context, owner, id string) (dbstore.UserRuntimeRecord, error) {
	if owner != f.registration.UserID || id != f.registration.ID {
		return dbstore.UserRuntimeRecord{}, db.ErrNotFound
	}
	return f.registration, nil
}

func (*delegationPlanFixture) OwnedHTTPMCPConnectionBot(context.Context, string, string) (string, error) {
	return "", errors.New("legacy MCP unavailable")
}

func (f *delegationPlanFixture) ListMounts(_ context.Context, bot string) ([]dbstore.BotRemoteRuntimeBindingRecord, error) {
	return []dbstore.BotRemoteRuntimeBindingRecord{f.mounts[bot+"/"+planSourceMount]}, nil
}

func (f *delegationPlanFixture) DelegationMountForRuntime(_ context.Context, p sqlc.DelegationMountForRuntimeParams) (pgtype.UUID, error) {
	f.lookups++
	if f.missingTarget || p.BotID != db.ParseUUIDOrEmpty(planTargetBot) || p.RuntimeID != db.ParseUUIDOrEmpty(planRuntime) {
		return pgtype.UUID{}, pgx.ErrNoRows
	}
	return db.ParseUUIDOrEmpty(planTargetMount), nil
}

func TestNativeDelegationPlanUsesExistingDistinctMounts(t *testing.T) {
	for _, selected := range []string{"native", planSourceMount} {
		t.Run(selected, func(t *testing.T) {
			f, a, b := newDelegationPlanFixture(t, selected)
			pairs, target, err := planDelegationDevices(t.Context(), f, a, b, selected)
			want := planTargetMount
			if selected == "native" {
				want = "native"
			}
			if err != nil || target != want || len(pairs) != 1 {
				t.Fatalf("pairs=%+v target=%s err=%v", pairs, target, err)
			}
			p := pairs[0]
			decoded, err := agenvo.DecodeRuntimeTarget("memoh", planRuntime, f.registration.ProviderConfig)
			if err != nil || p.SourceMountID != planSourceMount || p.TargetMountID != planTargetMount || p.RuntimeID != planRuntime || p.Fingerprint != agenvo.TargetFingerprint(decoded) {
				t.Fatalf("unbound native pair=%+v %v", p, err)
			}
		})
	}
}

func TestNativeDelegationPlanFailsClosed(t *testing.T) {
	for _, selected := range []string{"native", planSourceMount} {
		for _, name := range []string{"missing target", "revoked", "wrong Bot", "wrong mount", "wrong runtime", "wrong provider", "bad profile", "inactive registry", "legacy provider", "chat revoked", "forged selection"} {
			t.Run(selected+"/"+name, func(t *testing.T) {
				f, a, b := newDelegationPlanFixture(t, selected)
				m := f.mounts[planTargetBot+"/"+planTargetMount]
				switch name {
				case "missing target":
					f.missingTarget = true
				case "revoked":
					m.RuntimeRevoked = true
				case "wrong Bot":
					m.BotID = planSourceBot
				case "wrong mount":
					m.ID = planSourceMount
				case "wrong runtime":
					m.RuntimeID = planSourceBot
				case "wrong provider":
					m.RuntimeProvider = "other"
				case "bad profile":
					m.RuntimeProviderConfig = []byte(`{}`)
				case "inactive registry":
					f.registration.ActivatedAt = time.Time{}
				case "legacy provider":
					s := f.mounts[planSourceBot+"/"+planSourceMount]
					s.RuntimeProvider = "agenvo"
					f.mounts[planSourceBot+"/"+planSourceMount] = s
				case "chat revoked":
					f.denied = true
				case "forged selection":
					b.SourceSelection = "forged"
				}
				f.mounts[planTargetBot+"/"+planTargetMount] = m
				pairs, target, err := planDelegationDevices(t.Context(), f, a, b, selected)
				if len(pairs) != 0 {
					t.Fatalf("unauthorized pair=%+v", pairs)
				}
				mustDeny := selected != "native" || name == "chat revoked" || name == "forged selection"
				if mustDeny && !errors.Is(err, botdelegation.ErrDenied) {
					t.Fatalf("target=%s err=%v", target, err)
				}
				if !mustDeny && (err != nil || target != "native") {
					t.Fatalf("native fallback=%s %v", target, err)
				}
			})
		}
	}
}
