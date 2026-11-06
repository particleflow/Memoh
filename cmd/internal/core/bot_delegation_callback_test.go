package core

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	sdk "github.com/felinics/twilight/sdk"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/agent/runtime/native"
	tools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/models"
)

const (
	callbackSourceSession = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	callbackImageHash     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

type (
	callbackMarker    struct{}
	callbackAuthority struct {
		*delegatedImageAuthority
		permissions *applicationBotPermissionChecker
		linked      bool
		pending     bool
		run         sqlc.SessionRun
		session     sqlc.BotSession
		callbacks   atomic.Int32
	}
)

// Use the real owner permission resolver for the specialist: default deny and
// no grants must not reject the owner's chat permission.
type callbackPermissionQueries struct {
	dbstore.Queries
	grants atomic.Int32
}

func (*callbackPermissionQueries) GetBotByID(_ context.Context, id pgtype.UUID) (sqlc.GetBotByIDRow, error) {
	if id.String() != imageSpawnBot {
		return sqlc.GetBotByIDRow{}, pgx.ErrNoRows
	}
	return sqlc.GetBotByIDRow{ID: id, OwnerUserID: db.ParseUUIDOrEmpty(imageSpawnUser), Name: "synthetic-specialist", IsActive: true, Metadata: []byte(`{}`)}, nil
}

func (*callbackPermissionQueries) GetBotACLDefaultEffect(context.Context, pgtype.UUID) (string, error) {
	return "deny", nil
}

func (q *callbackPermissionQueries) ListBotUserGrantsForUser(context.Context, sqlc.ListBotUserGrantsForUserParams) ([]sqlc.ListBotUserGrantsForUserRow, error) {
	q.grants.Add(1)
	return nil, nil
}

func (a *callbackAuthority) Allowed(ctx context.Context, user, bot, permission string) (bool, error) {
	if bot == imageSpawnBot {
		return a.permissions.HasBotPermission(ctx, bot, user, permission)
	}
	return a.delegatedImageAuthority.Allowed(ctx, user, bot, permission)
}

func (a *callbackAuthority) SessionHasAsset(ctx context.Context, bot, session, hash string) (bool, error) {
	a.callbacks.Add(1)
	if ctx.Value(callbackMarker{}) != "trusted" {
		return false, errors.New("callback context lost")
	}
	if bot != imageSpawnOtherBot || session != callbackSourceSession {
		return false, nil
	}
	if a.pending {
		return admittedDelegationInput(a.run, a.session, bot, hash), nil
	}
	return a.linked && hash == callbackImageHash, nil
}

type callbackBots struct{}

func (callbackBots) ListAccessible(context.Context, string) ([]bots.Bot, error) {
	return []bots.Bot{{ID: imageSpawnBot, Name: "synthetic-specialist"}}, nil
}

type callbackLedger struct {
	botdelegation.Ledger
	claims   atomic.Int32
	captured botdelegation.Record
}

func (*callbackLedger) ForTarget(context.Context, string, string) (botdelegation.Record, error) {
	return botdelegation.Record{}, pgx.ErrNoRows
}

func (l *callbackLedger) Claim(_ context.Context, r botdelegation.Record) (botdelegation.Record, bool, error) {
	l.claims.Add(1)
	l.captured = r
	return r, false, nil
}

func (*callbackLedger) TargetRun(context.Context, botdelegation.Record) (*botdelegation.RunObservation, error) {
	return nil, nil
}

type callbackHost struct {
	planner   delegationMountPlanner
	authority *botdelegation.Authority
}

func (callbackHost) SourceRoute(_ context.Context, b botdelegation.Identity) (botdelegation.ReturnRoute, error) {
	return botdelegation.ReturnRoute{Version: 1, Channel: "web", ChatID: b.SourceBotID, ReplyTarget: b.SourceBotID}, nil
}

func (h callbackHost) Plan(ctx context.Context, b botdelegation.Identity, selected string) ([]botdelegation.DevicePair, string, error) {
	if h.planner != nil {
		return planDelegationDevices(ctx, h.planner, h.authority, b, selected)
	}
	if selected != "native" {
		return nil, "", botdelegation.ErrDenied
	}
	return nil, "native", nil
}

func (callbackHost) CreateSession(context.Context, botdelegation.Record) (string, error) {
	panic("replayed fixture must not dispatch")
}

func (callbackHost) Run(context.Context, botdelegation.Record, func(string) error) (botdelegation.Result, string, error) {
	panic("replayed fixture must not dispatch")
}

func (callbackHost) Deliver(context.Context, botdelegation.Record) error {
	panic("replayed fixture must not deliver")
}

// Exercises a synthetic model's real native callback and built-in delegation
// provider. The ledger stub returns an existing claim, so no specialist executes.
func TestDelegationNativeCallbackRequiresSourceLinkedOriginal(t *testing.T) {
	for _, name := range []string{"linked", "unlinked", "pending_owned", "pending_missing_user", "remote_source_native_specialist"} {
		linked := name == "linked" || name == "pending_owned" || name == "remote_source_native_specialist"
		t.Run(name, func(t *testing.T) {
			f := &callbackAuthority{delegatedImageAuthority: &delegatedImageAuthority{active: true, hash: callbackImageHash, sessions: map[string]thread.Thread{
				callbackSourceSession: {ID: callbackSourceSession, BotID: imageSpawnOtherBot, TeamID: "synthetic-team", CreatedByUserID: imageSpawnUser, Type: thread.TypeChat, Metadata: map[string]any{"workspace_target_id": "native"}},
			}}, linked: name == "linked" || name == "remote_source_native_specialist"}
			if strings.HasPrefix(name, "pending_") {
				f.pending = true
				team := db.ParseUUIDOrEmpty("11111111-1111-4111-8111-111111111111")
				user := imageSpawnUser
				if name == "pending_missing_user" {
					user = ""
				}
				raw, _ := json.Marshal(map[string]any{"resume": map[string]any{"version": 1, "user_id": user, "input_asset_hashes": []string{callbackImageHash}}})
				f.run = sqlc.SessionRun{BotID: db.ParseUUIDOrEmpty(imageSpawnOtherBot), SessionID: db.ParseUUIDOrEmpty(callbackSourceSession), TeamID: team, FencingToken: 7, State: "running", InputJson: raw}
				f.session = sqlc.BotSession{ID: f.run.SessionID, BotID: f.run.BotID, TeamID: team, CreatedByUserID: db.ParseUUIDOrEmpty(imageSpawnUser), RuntimeFencingToken: 7}
			}
			queries := &callbackPermissionQueries{}
			f.permissions = &applicationBotPermissionChecker{bots: bots.NewService(slog.Default(), queries), accounts: accounts.NewService(slog.Default(), imageSpawnAccounts{})}
			ledger := &callbackLedger{}
			service := botdelegation.NewService(&botdelegation.Authority{Permissions: f, Sessions: f, Assets: f}, ledger, nil, nil, 0)
			defer service.Close()
			service.Host = callbackHost{}
			if name == "remote_source_native_specialist" {
				mounts, _, _ := newDelegationPlanFixture(t, planSourceMount)
				mounts.nativeTarget = true
				mounts.registration.UserID = imageSpawnUser
				mount := mounts.mounts[planSourceBot+"/"+planSourceMount]
				mount.BotID = imageSpawnOtherBot
				mount.BotOwnerUserID = imageSpawnUser
				mount.RuntimeUserID = imageSpawnUser
				mounts.mounts = map[string]dbstore.BotRemoteRuntimeBindingRecord{imageSpawnOtherBot + "/" + planSourceMount: mount}
				f.sessions[callbackSourceSession] = thread.Thread{ID: callbackSourceSession, BotID: imageSpawnOtherBot, TeamID: "synthetic-team", CreatedByUserID: imageSpawnUser, Type: thread.TypeChat, Metadata: map[string]any{"workspace_target_id": planSourceMount, "workspace_target": map[string]any{"target_id": planSourceMount}}}
				service.Authority.Mounts = mounts
				service.Host = callbackHost{planner: mounts, authority: service.Authority}
			}
			var requests atomic.Int32
			var received string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var in map[string]any
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
					t.Error(err)
					return
				}
				switch requests.Add(1) {
				case 1:
					imageSpawnReply(w, false, "list_delegate_bots", map[string]any{}, "")
				case 2:
					if !strings.Contains(imageSpawnLastTool(in), imageSpawnBot) {
						t.Error("authorized specialist not listed")
					}
					imageSpawnReply(w, false, "delegate_to_bot", map[string]any{"target_bot_id": imageSpawnBot, "message": "synthetic task", "attachments": []any{map[string]any{"content_hash": callbackImageHash}}}, "")
				case 3:
					received = imageSpawnLastTool(in)
					imageSpawnReply(w, false, "", nil, "synthetic end")
				default:
					t.Error("unexpected callback")
					w.WriteHeader(http.StatusInternalServerError)
				}
			}))
			defer server.Close()
			a := native.New(native.Deps{})
			a.SetToolProviders(delegationScopedTools([]tools.ToolProvider{tools.NewBotDelegationProvider(service, callbackBots{})}))
			model := models.NewSDKChatModel(models.SDKModelConfig{ModelID: "synthetic-model", ClientType: "openai-completions", BaseURL: server.URL + "/v1"})
			ctx := context.WithValue(t.Context(), callbackMarker{}, "trusted")
			result, err := a.Generate(ctx, native.RunConfig{Model: model, SupportsToolCall: true, SessionType: "chat", Messages: []sdk.Message{sdk.UserMessage("synthetic request")}, Identity: native.SessionContext{BotID: imageSpawnOtherBot, UserID: imageSpawnUser, SessionID: callbackSourceSession}})
			if err != nil || result.Text != "synthetic end" {
				t.Fatalf("native callback: %v", err)
			}
			if queries.grants.Load() != 0 {
				t.Fatal("owner permission unexpectedly used explicit grants")
			}
			if requests.Load() != 3 || f.callbacks.Load() == 0 {
				t.Fatal("model callback did not reach attachment authorization")
			}
			if linked {
				if ledger.claims.Load() != 1 || ledger.captured.Identity.UserID != imageSpawnUser || ledger.captured.Identity.SourceSessionID != callbackSourceSession || len(ledger.captured.Payload.Inputs) != 1 || ledger.captured.Payload.Inputs[0] != callbackImageHash || strings.Contains(received, "session.not_found") {
					t.Fatalf("linked source failed: %s", received)
				}
				if name == "remote_source_native_specialist" && (ledger.captured.SourceSelection != planSourceMount || ledger.captured.Identity.SourceSelection != planSourceMount || ledger.captured.Payload.TargetSelection != "native" || len(ledger.captured.Payload.Devices) != 0) {
					t.Fatal("source provenance or specialist native execution lost")
				}
			} else if ledger.claims.Load() != 0 || !strings.Contains(received, "session.not_found") {
				t.Fatalf("unlinked source did not fail before claim: %s", received)
			}
		})
	}
}
