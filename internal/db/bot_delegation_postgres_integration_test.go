//go:build integration

package db_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/team"
)

func TestBotDelegationPostgresClaimIsolationAndDescendantAssets(t *testing.T) {
	ctx := t.Context()
	pool := freshMigratedDB(t)
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("SELECT set_config('memoh.team_id',$1,false)", team.DefaultTeamID)
	user, other, source, target := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, id := range []string{user, other} {
		exec("INSERT INTO users(id,username) VALUES($1::uuid,$1::text)", id)
		exec("INSERT INTO team_members(user_id) VALUES($1)", id)
	}
	for _, id := range []string{source, target} {
		exec("INSERT INTO bots(id,owner_user_id,name) VALUES($1::uuid,$2,$1::text)", id, user)
	}
	root, destination, child, grand, foreign, hidden, sibling := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, r := range []struct{ id, bot, user, parent, kind string }{{root, source, user, "", "chat"}, {destination, target, user, "", "chat"}, {child, target, user, destination, "subagent"}, {grand, target, user, child, "subagent"}, {foreign, target, other, destination, "subagent"}, {hidden, target, user, foreign, "subagent"}, {sibling, target, user, "", "chat"}} {
		exec("INSERT INTO bot_sessions(id,bot_id,created_by_user_id,parent_session_id,type) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5)", r.id, r.bot, r.user, r.parent, r.kind)
	}
	q := sqlc.New(conn)
	ledger := botdelegation.NewPostgresLedger(q)
	r := botdelegation.Record{ID: uuid.NewString(), Identity: botdelegation.Identity{TeamID: team.DefaultTeamID, UserID: user, SourceBotID: source, SourceSessionID: root, SourceRootID: root, TargetBotID: target}, RequestID: uuid.NewString(), RunID: uuid.NewString(), Fingerprint: "synthetic", SourceSelection: "native", Payload: botdelegation.Payload{Message: "synthetic task", TargetSelection: "native"}}
	r, created, err := ledger.Claim(ctx, r)
	if err != nil || !created {
		t.Fatalf("claim: %v %v", created, err)
	}
	replay := r
	replay.ID = uuid.NewString()
	replay.RequestID = uuid.NewString()
	got, created, err := ledger.Claim(ctx, replay)
	if err != nil || created || got.ID != r.ID {
		t.Fatalf("unsettled claim duplicated: %v %v", created, err)
	}
	for _, scope := range []struct{ bot, user, session string }{{target, user, root}, {source, other, root}, {source, user, destination}} {
		if _, err := ledger.Get(ctx, scope.bot, scope.user, scope.session, r.ID); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("foreign read: %v", err)
		}
	}
	stale := r
	r.Identity.TargetSessionID = destination
	r.State = "completed"
	r.DeliveryState = "unknown"
	r, err = ledger.Save(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ledger.Save(ctx, stale); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("stale revision saved: %v", err)
	}
	if got, err = ledger.ForTarget(ctx, target, destination); err != nil || got.ID != r.ID {
		t.Fatalf("durable target association: %v", err)
	}
	for i, entry := range []struct {
		session, role string
		visible       bool
	}{{destination, "assistant", true}, {child, "assistant", true}, {grand, "assistant", true}, {foreign, "assistant", true}, {hidden, "assistant", true}, {sibling, "assistant", true}, {child, "assistant", false}, {child, "tool", true}} {
		msg := uuid.NewString()
		hash := strings.Repeat(string(rune('a'+i)), 64)
		exec("INSERT INTO bot_history_messages(id,bot_id,session_id,role,content,turn_visible) VALUES($1,$2,$3,$4,'[]'::jsonb,$5)", msg, target, entry.session, entry.role, entry.visible)
		exec("INSERT INTO bot_history_message_assets(message_id,role,ordinal,content_hash) VALUES($1,'attachment',0,$2)", msg, hash)
	}
	refs, err := q.DelegationResultAssets(ctx, sqlc.DelegationResultAssetsParams{BotID: db.ParseUUIDOrEmpty(target), UserID: db.ParseUUIDOrEmpty(user), RootID: db.ParseUUIDOrEmpty(destination)})
	if err != nil || len(refs) != 3 {
		t.Fatalf("owned descendants: %v %v", refs, err)
	}
	rows, err := ledger.List(ctx, botdelegation.Filter{Bot: source, User: user, Session: root, Snapshot: time.Now().UTC(), Limit: 2})
	if err != nil || len(rows) != 1 {
		t.Fatalf("list: %d %v", len(rows), err)
	}
	foreignTeam := uuid.NewString()
	exec("SELECT set_config('memoh.team_id',$1,false)", foreignTeam)
	if _, err = ledger.Get(ctx, source, user, root, r.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-team read: %v", err)
	}
	exec("SELECT set_config('memoh.team_id',$1,false)", team.DefaultTeamID)
	var rls, forced bool
	if err = conn.QueryRow(ctx, "SELECT relrowsecurity,relforcerowsecurity FROM pg_class WHERE oid='bot_delegations'::regclass").Scan(&rls, &forced); err != nil || !rls || !forced {
		t.Fatalf("RLS not forced: %v", err)
	}
}
