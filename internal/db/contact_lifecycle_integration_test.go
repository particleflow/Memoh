//go:build integration

package db_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felinics/memoh/internal/channel/route"
	"github.com/felinics/memoh/internal/chat/event"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/hooks"
	"github.com/felinics/memoh/internal/team"
	"github.com/felinics/memoh/internal/workspace/bridge"
)

type contactLifecycleProbe struct {
	dbstore.Queries
	base                  *postgresstore.Queries
	beforeLock, afterLock func()
	lookupErr             error
}

func (*contactLifecycleProbe) SupportsTransactions() bool { return true }
func (q *contactLifecycleProbe) InTx(ctx context.Context, f func(dbstore.Queries) error) error {
	return q.base.InTx(ctx, func(tx dbstore.Queries) error {
		staged := *q
		staged.Queries = tx
		return f(&staged)
	})
}

func (q *contactLifecycleProbe) FindChatRoute(ctx context.Context, p sqlc.FindChatRouteParams) (sqlc.FindChatRouteRow, error) {
	if q.lookupErr != nil {
		return sqlc.FindChatRouteRow{}, q.lookupErr
	}
	return q.Queries.FindChatRoute(ctx, p)
}

func (q *contactLifecycleProbe) LockSessionForContact(ctx context.Context, p sqlc.LockSessionForContactParams) (sqlc.BotSession, error) {
	if q.beforeLock != nil {
		q.beforeLock()
	}
	s, err := q.Queries.LockSessionForContact(ctx, p)
	if err == nil && q.afterLock != nil {
		q.afterLock()
	}
	return s, err
}

// This provider counts actual native hook invocations at their workspace
// boundary and deliberately fails before any workspace or command can run.
type contactHookProbe struct{ calls atomic.Int64 }

func (p *contactHookProbe) MCPClient(context.Context, string) (*bridge.Client, error) {
	p.calls.Add(1)
	return nil, errors.New("synthetic hook workspace is unavailable")
}

type contactEventProbe struct{ calls atomic.Int64 }

func (p *contactEventProbe) Publish(e event.Event) {
	if e.Type == event.EventTypeSessionCreated {
		p.calls.Add(1)
	}
}

type contactLifecycleFixture struct {
	pool      *pgxpool.Pool
	q         *postgresstore.Queries
	sessions  *thread.Service
	bot, user string
	hook      *contactHookProbe
	events    *contactEventProbe
}

func newContactLifecycleFixture(t *testing.T, pool *pgxpool.Pool) contactLifecycleFixture {
	t.Helper()
	user, bot := uuid.NewString(), uuid.NewString()
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO users(id,username) VALUES($1::uuid,$1::text)", []any{user}},
		{"INSERT INTO team_members(user_id) VALUES($1)", []any{user}},
		{"INSERT INTO bots(id,owner_user_id,name) VALUES($1::uuid,$2,$1::text)", []any{bot, user}},
	} {
		if _, err := pool.Exec(t.Context(), statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	q := postgresstore.NewQueriesWithPool(pool, sqlc.New(pool))
	hook, events := &contactHookProbe{}, &contactEventProbe{}
	log := slog.New(slog.DiscardHandler)
	sessions := thread.NewService(log, q, events)
	sessions.SetHookService(hooks.NewService(log, hook))
	return contactLifecycleFixture{pool, q, sessions, bot, user, hook, events}
}

func (f contactLifecycleFixture) coordinator(q dbstore.Queries) *route.ThreadCoordinator {
	return route.NewThreadCoordinator(nil, route.NewService(nil, q), f.sessions)
}

func (f contactLifecycleFixture) resolve(t *testing.T) thread.Thread {
	t.Helper()
	s, err := f.coordinator(f.q).EnsurePrivateChat(t.Context(), f.bot, f.user)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (f contactLifecycleFixture) count(t *testing.T, table, predicate string, args ...any) int {
	t.Helper()
	var count int
	if err := f.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table+" WHERE "+predicate, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func awaitContactSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(10 * time.Second):
		t.Fatal("contact probe was not reached")
	}
}

func awaitContactLock(t *testing.T, pool *pgxpool.Pool, pid uint32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := pool.QueryRow(ctx, "SELECT COALESCE(wait_event_type='Lock',false) FROM pg_stat_activity WHERE pid=$1", pid).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("PostgreSQL lock wait was not observed")
		case <-ticker.C:
		}
	}
}

func TestPostgresPrivateChatLifecycle(t *testing.T) {
	_ = freshMigratedDB(t)
	cfg, err := pgxpool.ParseConfig(teamMigrationDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["memoh.team_id"] = team.DefaultTeamID
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	t.Run("ordinary_role_cannot_resolve_a_foreign_team_contact", func(t *testing.T) {
		f := newContactLifecycleFixture(t, pool)
		original := f.resolve(t)
		conn := rlsConn(t, pool, teamMigrationDSN(t))
		var role string
		if err := conn.QueryRow(t.Context(), "SELECT current_user").Scan(&role); err != nil {
			t.Fatal(err)
		}
		isolated := cfg.Copy()
		isolated.ConnConfig.RuntimeParams["role"] = role
		restricted, err := pgxpool.NewWithConfig(t.Context(), isolated)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(restricted.Close)
		q := postgresstore.NewQueriesWithPool(restricted, sqlc.New(restricted))
		if got, err := f.coordinator(q).EnsurePrivateChat(t.Context(), f.bot, f.user); err != nil || got.ID != original.ID {
			t.Fatalf("ordinary role lost its own contact: %v", err)
		}
		foreignTeam := uuid.NewString()
		if _, err := pool.Exec(t.Context(), "INSERT INTO teams(id,slug) VALUES($1::uuid,$1::text)", foreignTeam); err != nil {
			t.Fatal(err)
		}
		isolated.ConnConfig.RuntimeParams["memoh.team_id"] = foreignTeam
		foreign, err := pgxpool.NewWithConfig(t.Context(), isolated)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(foreign.Close)
		foreignQ := postgresstore.NewQueriesWithPool(foreign, sqlc.New(foreign))
		if _, err := foreignQ.GetSessionByID(t.Context(), db.ParseUUIDOrEmpty(original.ID)); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("foreign team read a session: %v", err)
		}
		if _, err := f.coordinator(foreignQ).EnsurePrivateChat(t.Context(), f.bot, f.user); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("foreign team resolved a contact: %v", err)
		}
		var count int
		if err := foreign.QueryRow(t.Context(), "SELECT count(*) FROM bot_channel_routes").Scan(&count); err != nil || count != 0 {
			t.Fatalf("foreign route count=%d error=%v", count, err)
		}
	})

	t.Run("staged_notification_is_safe_to_invoke_more_than_once", func(t *testing.T) {
		f := newContactLifecycleFixture(t, pool)
		var notify func()
		if err := f.q.InTx(t.Context(), func(q dbstore.Queries) error {
			_, staged, err := f.sessions.StageRouteSession(t.Context(), q, thread.CreateInput{BotID: f.bot, CreatedByUserID: f.user})
			notify = staged
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if f.hook.calls.Load() != 0 || f.events.calls.Load() != 0 {
			t.Fatal("staging published before commit")
		}
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(notify)
		}
		wg.Wait()
		if f.hook.calls.Load() != 1 || f.events.calls.Load() != 1 {
			t.Fatal("staged callback was invoked more than once")
		}
	})

	t.Run("activation_failure_rolls_back_without_notifications", func(t *testing.T) {
		f := newContactLifecycleFixture(t, pool)
		var activations, commits atomic.Int64
		q := &contactFaultQueries{Queries: f.q, base: f.q, failActivation: true, activationCalls: &activations, commitCalls: &commits}
		if _, err := f.coordinator(q).EnsurePrivateChat(t.Context(), f.bot, f.user); !errors.Is(err, errContactActivationProbe) {
			t.Fatalf("wrong failure: %v", err)
		}
		if activations.Load() != 1 || commits.Load() != 0 || f.hook.calls.Load() != 0 || f.events.calls.Load() != 0 || f.count(t, "bot_sessions", "bot_id=$1", f.bot) != 0 || f.count(t, "bot_channel_routes", "bot_id=$1", f.bot) != 0 {
			t.Fatal("rollback had side effects or missed the activation probe")
		}
	})

	t.Run("known_commit_hook_once_and_unknown_commit_never_replayed", func(t *testing.T) {
		f := newContactLifecycleFixture(t, pool)
		first := f.resolve(t)
		if f.resolve(t).ID != first.ID || f.hook.calls.Load() != 1 || f.events.calls.Load() != 1 {
			t.Fatal("contact reuse replayed lifecycle callbacks")
		}
		unknown := newContactLifecycleFixture(t, pool)
		var commits atomic.Int64
		q := &contactFaultQueries{Queries: unknown.q, base: unknown.q, unknownCommit: true, commitCalls: &commits}
		if _, err := unknown.coordinator(q).EnsurePrivateChat(t.Context(), unknown.bot, unknown.user); !errors.Is(err, db.ErrCommitOutcomeUnknown) {
			t.Fatalf("unknown commit: %v", err)
		}
		if commits.Load() != 1 || unknown.hook.calls.Load() != 0 || unknown.events.calls.Load() != 0 {
			t.Fatal("unknown commit ran or retried a callback")
		}
		recovered := unknown.resolve(t)
		if unknown.resolve(t).ID != recovered.ID || unknown.count(t, "bot_sessions", "bot_id=$1", unknown.bot) != 1 || unknown.hook.calls.Load() != 0 || unknown.events.calls.Load() != 0 {
			t.Fatal("reconciliation replayed creation or a callback")
		}
	})

	t.Run("descriptor_changes_keep_the_original_contact", func(t *testing.T) {
		f := newContactLifecycleFixture(t, pool)
		original := f.resolve(t)
		changed, err := f.sessions.UpdateEmptyDescriptorAndMetadataWithOwner(t.Context(), original.ID, "discuss", "discuss", "model", nil, nil, nil, f.user)
		if err != nil {
			t.Fatal(err)
		}
		if got := f.resolve(t); got.ID != original.ID || got.SessionMode != changed.SessionMode {
			t.Fatal("mode change replaced or broke contact")
		}
		_, err = f.sessions.UpdateEmptyDescriptorAndMetadataWithOwner(t.Context(), original.ID, "chat", "chat", "codex", nil, nil, nil, f.user)
		if err != nil {
			t.Fatal(err)
		}
		if got := f.resolve(t); got.ID != original.ID || got.RuntimeType != "codex" || f.hook.calls.Load() != 1 {
			t.Fatal("runtime change replaced or broke contact")
		}
	})

	t.Run("read_error_and_empty_pointer_do_not_authorize_creation", func(t *testing.T) {
		f := newContactLifecycleFixture(t, pool)
		original := f.resolve(t)
		outage := errors.New("synthetic route read outage")
		if _, err := f.coordinator(&contactLifecycleProbe{Queries: f.q, base: f.q, lookupErr: outage}).EnsurePrivateChat(t.Context(), f.bot, f.user); !errors.Is(err, outage) {
			t.Fatalf("read error was treated as absence: %v", err)
		}
		if err := f.q.SetRouteActiveSession(t.Context(), sqlc.SetRouteActiveSessionParams{ID: db.ParseUUIDOrEmpty(original.RouteID)}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.coordinator(f.q).EnsurePrivateChat(t.Context(), f.bot, f.user); !errors.Is(err, route.ErrContactUnavailable) {
			t.Fatalf("empty pointer was treated as absence: %v", err)
		}
		if f.count(t, "bot_sessions", "bot_id=$1", f.bot) != 1 || f.hook.calls.Load() != 1 {
			t.Fatal("uncertain state created a chat")
		}
	})

	t.Run("owned_tombstone_rebuilds_once_foreign_tombstone_does_not", func(t *testing.T) {
		f := newContactLifecycleFixture(t, pool)
		original := f.resolve(t)
		if err := f.sessions.SoftDelete(t.Context(), original.ID); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		results := make(chan thread.Thread, 8)
		errs := make(chan error, 8)
		for range 8 {
			wg.Go(func() {
				s, err := f.coordinator(f.q).EnsurePrivateChat(t.Context(), f.bot, f.user)
				if err != nil {
					errs <- err
				} else {
					results <- s
				}
			})
		}
		wg.Wait()
		close(results)
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		canonical := ""
		for s := range results {
			if s.ID == original.ID || (canonical != "" && canonical != s.ID) {
				t.Fatal("tombstone replacement split contacts")
			}
			canonical = s.ID
		}
		if canonical == "" || f.count(t, "bot_sessions", "bot_id=$1 AND deleted_at IS NULL", f.bot) != 1 || f.hook.calls.Load() != 2 {
			t.Fatal("replacement was missing or duplicated")
		}
		foreign := newContactLifecycleFixture(t, pool)
		other, err := f.sessions.Create(t.Context(), thread.CreateInput{BotID: f.bot, CreatedByUserID: foreign.user, ChannelType: "web", RouteID: original.RouteID})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.sessions.SoftDelete(t.Context(), other.ID); err != nil {
			t.Fatal(err)
		}
		if err := f.q.SetRouteActiveSession(t.Context(), sqlc.SetRouteActiveSessionParams{ID: db.ParseUUIDOrEmpty(original.RouteID), ActiveSessionID: db.ParseUUIDOrEmpty(other.ID)}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.coordinator(f.q).EnsurePrivateChat(t.Context(), f.bot, f.user); !errors.Is(err, route.ErrContactUnavailable) {
			t.Fatalf("foreign tombstone accepted: %v", err)
		}
		if f.count(t, "bot_sessions", "bot_id=$1", f.bot) != 3 {
			t.Fatal("foreign tombstone caused creation")
		}
	})

	for _, kind := range []string{"route_delete", "runtime_clear", "bot_delete"} {
		t.Run("resolver_before_"+kind, func(t *testing.T) {
			f := newContactLifecycleFixture(t, pool)
			original := f.resolve(t)
			locked, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			q := &contactLifecycleProbe{Queries: f.q, base: f.q, afterLock: func() { close(locked); <-release }}
			result := make(chan error, 1)
			go func() {
				s, err := f.coordinator(q).EnsurePrivateChat(t.Context(), f.bot, f.user)
				if err == nil && s.ID != original.ID {
					err = errors.New("resolver replaced live contact")
				}
				result <- err
			}()
			awaitContactSignal(t, locked)
			// A separate backend can still lock Route while the resolver owns
			// Session. This is the actual regression assertion for lock order.
			probe, err := pool.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			_, probeErr := probe.Exec(t.Context(), "SELECT id FROM bot_channel_routes WHERE id=$1 FOR UPDATE NOWAIT", original.RouteID)
			_ = probe.Rollback(t.Context())
			if probeErr != nil {
				t.Fatalf("route locked before session lifecycle finished: %v", probeErr)
			}
			deleter, err := pool.Acquire(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			pid := deleter.Conn().PgConn().PID()
			deleted := make(chan error, 1)
			go func() {
				defer deleter.Release()
				q := sqlc.New(deleter)
				switch kind {
				case "route_delete":
					deleted <- q.DeleteChatRoute(t.Context(), db.ParseUUIDOrEmpty(original.RouteID))
				case "runtime_clear":
					deleted <- q.ClearBotRuntimeData(t.Context(), db.ParseUUIDOrEmpty(f.bot))
				case "bot_delete":
					deleted <- q.DeleteBotByID(t.Context(), db.ParseUUIDOrEmpty(f.bot))
				}
			}()
			awaitContactLock(t, pool, pid)
			once.Do(func() { close(release) })
			resolveErr, deleteErr := <-result, <-deleted
			if resolveErr != nil || deleteErr != nil {
				t.Fatalf("resolve=%v delete=%v", resolveErr, deleteErr)
			}
			if kind == "bot_delete" {
				if _, err := f.coordinator(f.q).EnsurePrivateChat(t.Context(), f.bot, f.user); !errors.Is(err, pgx.ErrNoRows) {
					t.Fatalf("deleted Bot recreated: %v", err)
				}
			} else {
				next := f.resolve(t)
				if next.ID == original.ID || f.count(t, "bot_channel_routes", "bot_id=$1 AND channel_type='web'", f.bot) != 1 {
					t.Fatal("deleted contact was not reconciled through one new route")
				}
			}
		})
	}

	for _, kind := range []string{"route_delete", "runtime_clear"} {
		t.Run(kind+"_before_resolver", func(t *testing.T) {
			f := newContactLifecycleFixture(t, pool)
			original := f.resolve(t)
			tx, err := pool.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			if _, err := tx.Exec(t.Context(), "SELECT id FROM bot_sessions WHERE id=$1 FOR UPDATE", original.ID); err != nil {
				t.Fatal(err)
			}
			started := make(chan struct{})
			q := &contactLifecycleProbe{Queries: f.q, base: f.q, beforeLock: func() { close(started) }}
			result := make(chan error, 1)
			go func() { _, err := f.coordinator(q).EnsurePrivateChat(t.Context(), f.bot, f.user); result <- err }()
			awaitContactSignal(t, started)
			if kind == "route_delete" {
				err = sqlc.New(tx).DeleteChatRoute(t.Context(), db.ParseUUIDOrEmpty(original.RouteID))
			} else {
				err = sqlc.New(tx).ClearBotRuntimeData(t.Context(), db.ParseUUIDOrEmpty(f.bot))
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := <-result; !errors.Is(err, route.ErrContactUnavailable) {
				t.Fatalf("stale binding blindly recreated: %v", err)
			}
			if f.count(t, "bot_channel_routes", "bot_id=$1", f.bot) != 0 {
				t.Fatal("uncertain read recreated route")
			}
			if f.resolve(t).ID == original.ID {
				t.Fatal("explicit reconciliation did not create a fresh contact")
			}
		})
	}
}
