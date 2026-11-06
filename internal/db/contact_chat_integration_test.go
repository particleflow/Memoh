//go:build integration

package db_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felinics/memoh/internal/channel/route"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/team"
)

type contactFaultQueries struct {
	dbstore.Queries
	base                          *postgresstore.Queries
	failActivation, unknownCommit bool
	activationCalls, commitCalls  *atomic.Int64
}

var errContactActivationProbe = errors.New("synthetic contact activation failure")

func (*contactFaultQueries) SupportsTransactions() bool { return true }
func (q *contactFaultQueries) InTx(ctx context.Context, f func(dbstore.Queries) error) error {
	err := q.base.InTx(ctx, func(tx dbstore.Queries) error {
		staged := *q
		staged.Queries = tx
		return f(&staged)
	})
	if err == nil && q.commitCalls != nil {
		q.commitCalls.Add(1)
	}
	if err == nil && q.unknownCommit {
		return db.ErrCommitOutcomeUnknown
	}
	return err
}

func (q *contactFaultQueries) SetRouteActiveSession(ctx context.Context, p dbsqlc.SetRouteActiveSessionParams) error {
	if q.activationCalls != nil {
		q.activationCalls.Add(1)
	}
	if q.failActivation {
		return errContactActivationProbe
	}
	return q.Queries.SetRouteActiveSession(ctx, p)
}

func TestPostgresPrivateChatIsolationAndConcurrentReuse(t *testing.T) {
	ctx := t.Context()
	_ = freshMigratedDB(t) // Existing helper creates a unique disposable test DB.
	cfg, err := pgxpool.ParseConfig(teamMigrationDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["memoh.team_id"] = team.DefaultTeamID
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	user, other, bot, otherBot := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, id := range []string{user, other} {
		exec("INSERT INTO users(id,username) VALUES($1::uuid,$1::text)", id)
		exec("INSERT INTO team_members(user_id) VALUES($1)", id)
	}
	for _, id := range []string{bot, otherBot} {
		exec("INSERT INTO bots(id,owner_user_id,name) VALUES($1::uuid,$2,$1::text)", id, user)
	}
	q := postgresstore.NewQueriesWithPool(pool, dbsqlc.New(pool))
	sessions := thread.NewService(nil, q, nil)
	old, err := sessions.Create(ctx, thread.CreateInput{BotID: bot, CreatedByUserID: user, ChannelType: "web", Title: "Delegated task"})
	if err != nil {
		t.Fatal(err)
	}
	var before string
	if err := pool.QueryRow(ctx, "SELECT to_jsonb(s)::text FROM bot_sessions s WHERE id=$1", old.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	coordinator := func(queries dbstore.Queries) *route.ThreadCoordinator {
		return route.NewThreadCoordinator(nil, route.NewService(nil, queries), sessions)
	}
	var wg sync.WaitGroup
	ids, errs := make(chan string, 8), make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			s, err := coordinator(q).EnsurePrivateChat(ctx, bot, user)
			if err != nil {
				errs <- err
			} else {
				ids <- s.ID
			}
		})
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	canonical := ""
	for id := range ids {
		if canonical != "" && id != canonical {
			t.Fatal("concurrent devices created different contacts")
		}
		canonical = id
	}
	if canonical == "" || canonical == old.ID {
		t.Fatal("missing contact or silently adopted legacy chat")
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM bot_sessions WHERE bot_id=$1 AND route_id IS NOT NULL", bot).Scan(&count); err != nil || count != 1 {
		t.Fatalf("contact sessions=%d err=%v", count, err)
	}
	// A fresh coordinator reads persisted state, not an in-memory selection.
	again, err := coordinator(q).EnsurePrivateChat(ctx, bot, user)
	if err != nil || again.ID != canonical {
		t.Fatalf("reconnect changed contact: %v", err)
	}
	foreignUser, err := coordinator(q).EnsurePrivateChat(ctx, bot, other)
	if err != nil || foreignUser.ID == canonical {
		t.Fatalf("mixed accounts: %v", err)
	}
	foreignBot, err := coordinator(q).EnsurePrivateChat(ctx, otherBot, user)
	if err != nil || foreignBot.ID == canonical {
		t.Fatalf("mixed Bots: %v", err)
	}
	var after string
	if err := pool.QueryRow(ctx, "SELECT to_jsonb(s)::text FROM bot_sessions s WHERE id=$1", old.ID).Scan(&after); err != nil || after != before {
		t.Fatalf("legacy chat changed: %v", err)
	}
	// Other channel routes are not reused, even for the same account/Bot.
	legacy, err := route.NewService(nil, q).Create(ctx, route.CreateInput{BotID: bot, Platform: "telegram", ExternalConversationID: "contact:" + user})
	if err != nil || legacy.ID == again.RouteID {
		t.Fatalf("mixed channel routes: %v", err)
	}
	// Pointer tampering never discloses another account's chat or silently repairs it.
	exec("UPDATE bot_channel_routes SET active_session_id=$1 WHERE id=$2", foreignUser.ID, again.RouteID)
	if _, err := coordinator(q).EnsurePrivateChat(ctx, bot, user); !errors.Is(err, route.ErrContactUnavailable) {
		t.Fatalf("foreign pointer accepted: %v", err)
	}
	// Activation failure rolls back both new route and session.
	failedUser := uuid.NewString()
	exec("INSERT INTO users(id,username) VALUES($1::uuid,$1::text)", failedUser)
	exec("INSERT INTO team_members(user_id) VALUES($1)", failedUser)
	var activations, commits atomic.Int64
	if _, err := coordinator(&contactFaultQueries{Queries: q, base: q, failActivation: true, activationCalls: &activations, commitCalls: &commits}).EnsurePrivateChat(ctx, bot, failedUser); !errors.Is(err, errContactActivationProbe) {
		t.Fatalf("activation injection not reached: %v", err)
	}
	if activations.Load() != 1 || commits.Load() != 0 {
		t.Fatalf("activation=%d commits=%d", activations.Load(), commits.Load())
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM bot_sessions WHERE created_by_user_id=$1", failedUser).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback left a chat: %d %v", count, err)
	}
	// A lost commit acknowledgement is reconciled through the same route/key.
	unknownUser := uuid.NewString()
	exec("INSERT INTO users(id,username) VALUES($1::uuid,$1::text)", unknownUser)
	exec("INSERT INTO team_members(user_id) VALUES($1)", unknownUser)
	activations.Store(0)
	if _, err := coordinator(&contactFaultQueries{Queries: q, base: q, unknownCommit: true, activationCalls: &activations, commitCalls: &commits}).EnsurePrivateChat(ctx, bot, unknownUser); !errors.Is(err, db.ErrCommitOutcomeUnknown) {
		t.Fatalf("lost acknowledgement=%v", err)
	}
	if activations.Load() != 1 || commits.Load() != 1 {
		t.Fatalf("unknown acknowledgement did not follow one real commit: activation=%d commits=%d", activations.Load(), commits.Load())
	}
	recovered, err := coordinator(q).EnsurePrivateChat(ctx, bot, unknownUser)
	if err != nil || recovered.ID == "" {
		t.Fatalf("commit reconciliation failed: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM bot_sessions WHERE created_by_user_id=$1", unknownUser).Scan(&count); err != nil || count != 1 {
		t.Fatalf("unknown commit duplicated chat: %d %v", count, err)
	}
}
