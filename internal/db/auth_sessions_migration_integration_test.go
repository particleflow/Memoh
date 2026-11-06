//go:build integration

package db_test

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/felinics/memoh/internal/db"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/team"
)

// authSessionPool migrates the test database and returns a pool. The single
// connection carries the team context, so seeding and team-scoped store calls
// share one backend session.
func authSessionPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := teamMigrationDSN(t)
	if err := db.RunMigrate(slog.New(slog.DiscardHandler), pgConfigFromDSN(t, dsn), postgresMigrationsFS(t), "up", nil); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(context.Background(), "SELECT set_config('memoh.team_id',$1,false)", team.DefaultTeamID); err != nil {
		t.Fatalf("set team context: %v", err)
	}
	return pool
}

// authSessionConcurrentPool is the same migrated database on a wider pool, for
// tests whose transactions must genuinely overlap.
func authSessionConcurrentPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := teamMigrationDSN(t)
	if err := db.RunMigrate(slog.New(slog.DiscardHandler), pgConfigFromDSN(t, dsn), postgresMigrationsFS(t), "up", nil); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func authSessionInput(userID, deviceID string, expiresAt time.Time) dbstore.CreateAuthSessionInput {
	return dbstore.CreateAuthSessionInput{UserID: userID, DeviceID: deviceID, RefreshHash: fmt.Sprintf("hash-%s", uuid.NewString()), ExpiresAt: expiresAt}
}

func storePasswordInput(userID, hash string) dbstore.UpdateAccountPasswordInput {
	return dbstore.UpdateAccountPasswordInput{UserID: userID, PasswordHash: hash}
}

// seedAuthUser inserts a user with a team membership and returns its id. The
// inserts run in one transaction with the team context set, so they work on
// any pooled connection.
func seedAuthUser(t *testing.T, pool *pgxpool.Pool, username string) string {
	t.Helper()
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT set_config('memoh.team_id',$1,true)", team.DefaultTeamID); err != nil {
		t.Fatalf("set team context: %v", err)
	}
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO users (username, password_hash) VALUES ($1, 'x') RETURNING id`, username).Scan(&id); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO team_members(user_id) VALUES($1)", id); err != nil {
		t.Fatalf("insert membership: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return id
}

func authSessionRowCount(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var count int
	require.NoError(t, pool.QueryRow(context.Background(), query, args...).Scan(&count))
	return count
}

func authTableExists(t *testing.T, pool *pgxpool.Pool, table string) bool {
	t.Helper()
	var exists bool
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name=$1)`, table).Scan(&exists))
	return exists
}

// The full chain applies 0184 and then 0185 in order, the new table stays
// account-level (no RLS), and the down migration of 0185 keeps the table.
func TestAuthSessionsMigration0184Then0185(t *testing.T) {
	pool := authSessionPool(t)
	dsn := teamMigrationDSN(t)
	ctx := context.Background()

	// The chain head is 0185 and 0184's table arrived with it.
	assert.True(t, authTableExists(t, pool, "conversation_presence_decisions"))
	assert.True(t, authTableExists(t, pool, "auth_sessions"))

	// Stepping back and forward exercises the 0184 -> 0185 pair directly:
	// down keeps the table (no-op), up re-applies over the retained table.
	stepDown(t, dsn, 1)
	assert.True(t, authTableExists(t, pool, "auth_sessions"))
	stepUp(t, dsn, 1)

	// Account-level like users: no row level security.
	var rls bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT relrowsecurity FROM pg_class WHERE oid='public.auth_sessions'::regclass`).Scan(&rls))
	assert.False(t, rls, "auth_sessions must not have RLS")

	// The partial unique index exists with its predicate.
	var predicate string
	require.NoError(t, pool.QueryRow(ctx, `SELECT pg_get_expr(i.indpred, i.indrelid) FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid WHERE c.relname='auth_sessions_active_device'`).Scan(&predicate))
	assert.Contains(t, predicate, "revoked_at IS NULL")
	assert.Contains(t, predicate, "device_id <> ''")
}

func TestAuthSessionStoreDeviceReplacementAndConcurrency(t *testing.T) {
	pool := authSessionConcurrentPool(t)
	ctx := context.Background()
	st, err := postgresstore.New(pool)
	require.NoError(t, err)

	user := seedAuthUser(t, pool, "auth-sessions-owner")
	other := seedAuthUser(t, pool, "auth-sessions-other")
	expires := time.Now().Add(24 * time.Hour)

	first, err := st.CreateAuthSession(ctx, authSessionInput(user, "device-1", expires))
	require.NoError(t, err)
	second, err := st.CreateAuthSession(ctx, authSessionInput(user, "device-1", expires))
	require.NoError(t, err)
	assert.NotEqual(t, first.ID, second.ID)

	// One live row per device; the replaced one is revoked.
	assert.Equal(t, 1, authSessionRowCount(t, pool, `SELECT count(*) FROM auth_sessions WHERE user_id=$1 AND device_id='device-1' AND revoked_at IS NULL`, user))
	assert.Equal(t, 1, authSessionRowCount(t, pool, `SELECT count(*) FROM auth_sessions WHERE id=$1 AND revoked_at IS NOT NULL`, first.ID))

	// Empty device ids stay outside the uniqueness: several can be live.
	for i := 0; i < 3; i++ {
		_, err := st.CreateAuthSession(ctx, authSessionInput(user, "", expires))
		require.NoError(t, err)
	}
	assert.Equal(t, 3, authSessionRowCount(t, pool, `SELECT count(*) FROM auth_sessions WHERE user_id=$1 AND device_id='' AND revoked_at IS NULL`, user))

	// The same device id on another user coexists.
	_, err = st.CreateAuthSession(ctx, authSessionInput(other, "device-1", expires))
	require.NoError(t, err)

	// The designed race is the double login (double-click or resend): both
	// succeed, the loser's retry replaces the winner's row, one live row stays.
	t.Run("double login", func(t *testing.T) {
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := st.CreateAuthSession(ctx, authSessionInput(user, "device-double", expires))
				errs <- err
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			assert.NoError(t, err, "a two-way race must resolve through the retry")
		}
		assert.Equal(t, 1, authSessionRowCount(t, pool, `SELECT count(*) FROM auth_sessions WHERE user_id=$1 AND device_id='device-double' AND revoked_at IS NULL`, user))
	})

	// A wider race may still exhaust the single retry; what the contract
	// promises there is a clean conflict sentinel, never an unexpected error,
	// and still exactly one live row.
	t.Run("wide race", func(t *testing.T) {
		const workers = 8
		var wg sync.WaitGroup
		errs := make(chan error, workers)
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := st.CreateAuthSession(ctx, authSessionInput(user, "device-race", expires))
				errs <- err
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				assert.ErrorIs(t, err, db.ErrAuthSessionConflict, "a lost race must surface the conflict sentinel, not a raw error")
			}
		}
		assert.Equal(t, 1, authSessionRowCount(t, pool, `SELECT count(*) FROM auth_sessions WHERE user_id=$1 AND device_id='device-race' AND revoked_at IS NULL`, user))
	})
}

func TestAuthSessionStoreLifecycle(t *testing.T) {
	pool := authSessionPool(t)
	ctx := context.Background()
	st, err := postgresstore.New(pool)
	require.NoError(t, err)

	user := seedAuthUser(t, pool, "auth-sessions-lifecycle")
	session, err := st.CreateAuthSession(ctx, authSessionInput(user, "device-1", time.Now().Add(time.Minute)))
	require.NoError(t, err)

	require.NoError(t, st.GetActiveAuthSession(ctx, session.ID, user))
	require.Error(t, st.GetActiveAuthSession(ctx, session.ID, "99999999-9999-4999-8999-999999999999"), "a session of another user must not validate")

	touched, err := st.TouchAuthSession(ctx, session.ID, time.Now().Add(48*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(1), touched)

	rows, err := st.RevokeAuthSession(ctx, session.ID, user, "logout")
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows)

	require.Error(t, st.GetActiveAuthSession(ctx, session.ID, user))
	touched, err = st.TouchAuthSession(ctx, session.ID, time.Now().Add(48*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(0), touched, "a revoked session must not slide")

	rows, err = st.RevokeAuthSession(ctx, session.ID, user, "logout")
	require.NoError(t, err)
	assert.Equal(t, int64(0), rows, "revocation is idempotent")

	// Stale sessions older than the retention window are deleted; fresh ones stay.
	_, err = pool.Exec(ctx, `UPDATE auth_sessions SET expires_at = now() - interval '31 days' WHERE id=$1`, session.ID)
	require.NoError(t, err)
	fresh, err := st.CreateAuthSession(ctx, authSessionInput(user, "device-2", time.Now().Add(time.Hour)))
	require.NoError(t, err)
	deleted, err := st.DeleteStaleAuthSessions(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, deleted, int64(1))
	assert.Equal(t, 0, authSessionRowCount(t, pool, `SELECT count(*) FROM auth_sessions WHERE id=$1`, session.ID))
	assert.Equal(t, 1, authSessionRowCount(t, pool, `SELECT count(*) FROM auth_sessions WHERE id=$1`, fresh.ID))

	_, err = st.GetAuthSessionByID(ctx, fresh.ID)
	require.NoError(t, err)
}

func TestUpdatePasswordAndRevokeSessionsIsAtomic(t *testing.T) {
	pool := authSessionPool(t)
	ctx := context.Background()
	st, err := postgresstore.New(pool)
	require.NoError(t, err)

	user := seedAuthUser(t, pool, "auth-sessions-password")
	first, err := st.CreateAuthSession(ctx, authSessionInput(user, "device-1", time.Now().Add(time.Hour)))
	require.NoError(t, err)
	second, err := st.CreateAuthSession(ctx, authSessionInput(user, "device-2", time.Now().Add(time.Hour)))
	require.NoError(t, err)

	require.NoError(t, st.UpdatePasswordAndRevokeSessions(ctx, storePasswordInput(user, "new-hash")))
	var hash string
	require.NoError(t, pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1`, user).Scan(&hash))
	assert.Equal(t, "new-hash", hash)
	assert.Equal(t, 0, authSessionRowCount(t, pool, `SELECT count(*) FROM auth_sessions WHERE user_id=$1 AND revoked_at IS NULL`, user))
	assert.Equal(t, 2, authSessionRowCount(t, pool, `SELECT count(*) FROM auth_sessions WHERE id IN ($1,$2) AND revoked_at IS NOT NULL AND revoke_reason='password_changed'`, first.ID, second.ID))
}
