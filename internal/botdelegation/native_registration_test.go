package botdelegation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/toolcontext"
)

const nativeRuntimeID = "11111111-1111-4111-8111-111111111111"

func nativeFixture(t *testing.T) (*Authority, *authorityFixture, Identity, DevicePair) {
	t.Helper()
	a, f, b, p := fixture()
	root := f.sessions["root"]
	root.Metadata = map[string]any{"workspace_target_id": p.SourceMountID}
	f.sessions[root.ID] = root
	b.SourceSelection = p.SourceMountID
	return a, f, b, p
}

func TestNativeRegistrationBindsDistinctBotsAndReconstructsScope(t *testing.T) {
	a, _, b, p := nativeFixture(t)
	bound, selected, err := a.Bind(t.Context(), b.UserID, b.SourceBotID, b.SourceSessionID, b.TargetBotID)
	if err != nil || selected != p.SourceMountID || bound.SourceRootID != b.SourceRootID {
		t.Fatalf("native bind=%+v %q %v", bound, selected, err)
	}
	for _, permission := range []string{bots.PermissionWorkspaceRead, bots.PermissionWorkspaceWrite, bots.PermissionWorkspaceExec} {
		if err := a.AuthorizeDevice(t.Context(), b, p, permission); err != nil {
			t.Fatal(err)
		}
	}
	r := Record{Identity: b, SourceSelection: p.SourceMountID, Payload: Payload{Devices: []DevicePair{p}, TargetSelection: p.TargetMountID}}
	ctx := (&Service{Authority: a}).BindScope(t.Context(), r)
	if err := toolcontext.CheckDelegationTarget(ctx, b.TargetBotID, p.TargetMountID, ""); err != nil {
		t.Fatal(err)
	}
	if err := toolcontext.CheckDelegationTarget(ctx, b.SourceBotID, p.TargetMountID, bots.PermissionWorkspaceRead); !errors.Is(err, ErrDenied) {
		t.Fatalf("forged Bot=%v", err)
	}
}

func TestNativeRegistrationRejectsLiveChangesAndForgedIdentity(t *testing.T) {
	for _, name := range []string{"registration missing", "not activated", "registry provider", "registry owner", "registry id", "registry config", "revoked", "mount bot", "mount id", "runtime id", "owner", "provider", "bad target", "config changed", "profile replaced", "source permission", "target permission", "target chat", "source user", "target session"} {
		t.Run(name, func(t *testing.T) {
			a, f, b, p := nativeFixture(t)
			if err := a.AuthorizeDevice(t.Context(), b, p, bots.PermissionWorkspaceExec); err != nil {
				t.Fatal(err)
			}
			m := f.mounts["specialist/specialist-pc-a"]
			switch name {
			case "registration missing":
				f.registrationErr = ErrDenied
			case "not activated":
				f.registration.ActivatedAt = time.Time{}
			case "registry provider":
				f.registration.Provider = "retired"
			case "registry owner":
				f.registration.UserID = "other"
			case "registry id":
				f.registration.ID = "other"
			case "registry config":
				f.registration.ProviderConfig = []byte(`{"herdr":{}}`)
			case "revoked":
				m.RuntimeRevoked = true
			case "mount bot":
				m.BotID = b.SourceBotID
			case "mount id":
				m.ID = p.SourceMountID
			case "runtime id":
				m.RuntimeID = "22222222-2222-4222-8222-222222222222"
			case "owner":
				m.RuntimeUserID = "other"
			case "provider":
				m.RuntimeProvider = "unknown"
			case "bad target":
				m.RuntimeProviderConfig = []byte(`{"connection_id":"forged"}`)
			case "config changed":
				var v map[string]any
				if err := json.Unmarshal(m.RuntimeProviderConfig, &v); err != nil {
					t.Fatal(err)
				}
				v["herdr"].(map[string]any)["session"] = "changed"
				m.RuntimeProviderConfig, _ = json.Marshal(v)
			case "profile replaced":
				var v map[string]any
				if err := json.Unmarshal(m.RuntimeProviderConfig, &v); err != nil {
					t.Fatal(err)
				}
				v["herdr"].(map[string]any)["session"] = "replaced"
				raw, _ := json.Marshal(v)
				f.registration.ProviderConfig = raw
				for key, mount := range f.mounts {
					mount.RuntimeProviderConfig = raw
					f.mounts[key] = mount
				}
				m.RuntimeProviderConfig = raw
			case "source permission":
				f.denied["manager/workspace_exec"] = true
			case "target permission":
				f.denied["specialist/workspace_exec"] = true
			case "target chat":
				f.denied["specialist/chat"] = true
			case "source user":
				b.UserID = "other"
			case "target session":
				s := f.sessions[b.TargetSessionID]
				s.BotID = b.SourceBotID
				f.sessions[s.ID] = s
			}
			f.mounts["specialist/specialist-pc-a"] = m
			if err := a.AuthorizeDevice(t.Context(), b, p, bots.PermissionWorkspaceExec); !errors.Is(err, ErrDenied) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

type nativeDelegationHost struct {
	*hostFixture
	pair      DevicePair
	delivered Record
}

func (h *nativeDelegationHost) Plan(ctx context.Context, b Identity, _ string) ([]DevicePair, string, error) {
	p, err := h.service.Authority.PlanDevice(ctx, b, h.pair.SourceMountID, h.pair.TargetMountID)
	return []DevicePair{p}, p.TargetMountID, err
}

func (h *nativeDelegationHost) Run(ctx context.Context, r Record, started func(string) error) (Result, string, error) {
	scope := h.service.BindScope(ctx, r)
	for _, permission := range []string{"", bots.PermissionWorkspaceRead, bots.PermissionWorkspaceExec} {
		if err := toolcontext.CheckDelegationTarget(scope, r.Identity.TargetBotID, r.Payload.TargetSelection, permission); err != nil {
			return Result{}, "failed", err
		}
	}
	return h.hostFixture.Run(ctx, r, started)
}

func (h *nativeDelegationHost) Deliver(ctx context.Context, r Record) error {
	h.delivered = r
	return h.hostFixture.Deliver(ctx, r)
}

func TestNativeHandoffReturnsToSourceAndNeverRedispatches(t *testing.T) {
	for _, state := range []string{"completed", "unknown"} {
		t.Run(state, func(t *testing.T) {
			a, f, b, p := nativeFixture(t)
			target := "20000000-0000-4000-8000-000000000003"
			f.teams[target] = "team"
			delete(f.teams, "specialist")
			session := f.sessions["specialist-session"]
			session.BotID = target
			f.sessions[session.ID] = session
			mount := f.mounts["specialist/"+p.TargetMountID]
			mount.BotID = target
			delete(f.mounts, "specialist/"+p.TargetMountID)
			f.mounts[target+"/"+p.TargetMountID] = mount
			ledger := &memoryLedger{records: map[string]Record{}}
			s := NewService(a, ledger, nil, nil, time.Minute)
			defer s.Close()
			h := &nativeDelegationHost{hostFixture: &hostFixture{service: s, state: state, block: make(chan struct{})}, pair: p}
			s.Host = h
			in := Input{RequestID: uuid.NewString(), TargetBotID: target, Message: "synthetic native task"}
			first, err := s.Submit(t.Context(), b.SourceBotID, b.UserID, b.SourceSessionID, in)
			if err != nil {
				close(h.block)
				t.Fatal(err)
			}
			// Replay while accepted/running must not launch a second specialist.
			replay, err := s.Submit(t.Context(), b.SourceBotID, b.UserID, b.SourceSessionID, in)
			close(h.block)
			s.wg.Wait()
			if err != nil || first.ID != replay.ID {
				t.Fatalf("accepted replay=%+v %v", replay, err)
			}
			if state == "unknown" {
				in.RequestID = uuid.NewString()
			}
			replay, err = s.Submit(t.Context(), b.SourceBotID, b.UserID, b.SourceSessionID, in)
			if err != nil || replay.ID != first.ID || replay.State != state {
				t.Fatalf("settled replay=%+v %v", replay, err)
			}
			if h.runs.Load() != 1 || h.creates.Load() != 1 {
				t.Fatal("native specialist redispatched")
			}
			if state == "completed" {
				r := h.delivered
				if h.deliveries.Load() != 1 || replay.DeliveryState != "accepted" || replay.SourceSessionID != b.SourceSessionID || r.Identity.SourceSessionID != b.SourceSessionID || r.Identity.SourceRootID != b.SourceRootID || r.Payload.ReturnRoute.ReplyTarget != b.SourceBotID || r.Payload.TargetSelection != p.TargetMountID || r.Identity.TargetBotID != target || r.Identity.TargetSessionID != b.TargetSessionID {
					t.Fatalf("result lost original conversation: %+v", r)
				}
			} else if h.deliveries.Load() != 1 || replay.DeliveryState != "accepted" || replay.Summary != nil {
				t.Fatal("opted-in unknown notification missing or exposed an unconfirmed result")
			}
		})
	}
}

func TestNativeTextAndOriginalReadsDoNotGrantExecution(t *testing.T) {
	a, f, b, p := nativeFixture(t)
	f.denied["manager/workspace_exec"] = true
	f.denied["specialist/workspace_exec"] = true
	if _, err := a.Authorize(t.Context(), b); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	f.assets["manager/root/"+hash] = true
	if err := a.AuthorizeAsset(t.Context(), b, hash, false); err != nil {
		t.Fatal(err)
	}
	if err := a.AuthorizeDevice(t.Context(), b, p, bots.PermissionWorkspaceRead); err != nil {
		t.Fatal(err)
	}
	if err := a.AuthorizeDevice(t.Context(), b, p, bots.PermissionWorkspaceExec); !errors.Is(err, ErrDenied) {
		t.Fatalf("execution grant broadened: %v", err)
	}
	if _, err := a.PlanDevice(t.Context(), b, "other-source-mount", p.TargetMountID); !errors.Is(err, ErrDenied) {
		t.Fatalf("source selection broadened: %v", err)
	}
}
