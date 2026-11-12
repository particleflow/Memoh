//go:build integration

package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felinics/memoh/internal/albumpairs"
	"github.com/felinics/memoh/internal/albumwriteback"
	memohdb "github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/team"
)

// albumPairFixture drives the real pairing archive: migration 0190, the
// single-active-target constraint, the upsert/retire write and the RLS boundary.
type albumPairFixture struct {
	pool  *pgxpool.Pool
	conn  *pgxpool.Conn
	store *postgresstore.Store
	owner string
	bot   string
	other string
}

func newAlbumPairFixture(t *testing.T) albumPairFixture {
	t.Helper()
	pool := freshMigratedDB(t)
	conn, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Release)
	f := albumPairFixture{
		pool:  pool,
		conn:  conn,
		store: postgresstore.NewWithQueries(dbsqlc.New(conn)),
		owner: uuid.NewString(),
		bot:   uuid.NewString(),
		other: uuid.NewString(),
	}
	f.exec(t, "SELECT set_config('memoh.team_id',$1,false)", team.DefaultTeamID)
	f.exec(t, "INSERT INTO users(id,username) VALUES($1,'album-pair-test')", f.owner)
	f.exec(t, "INSERT INTO team_members(user_id) VALUES($1)", f.owner)
	for _, id := range []string{f.bot, f.other} {
		f.exec(t, "INSERT INTO bots(id,owner_user_id,name) VALUES($1,$2,$3)", id, f.owner, "album-pair-"+id[:8])
	}
	return f
}

func (f albumPairFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.conn.Exec(t.Context(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestAlbumPairUpsertRetiresThePreviousActiveTarget(t *testing.T) {
	f := newAlbumPairFixture(t)
	ctx := t.Context()

	first, err := f.store.UpsertAlbumPair(ctx, dbstore.AlbumPairRecord{UserID: f.owner, BotID: f.bot, PairKey: "first"})
	if err != nil || first.State != "active" || first.PairKey != "first" {
		t.Fatalf("first upsert = %+v, %v", first, err)
	}
	second, err := f.store.UpsertAlbumPair(ctx, dbstore.AlbumPairRecord{UserID: f.owner, BotID: f.bot, PairKey: "second"})
	if err != nil || second.State != "active" {
		t.Fatalf("second upsert = %+v, %v", second, err)
	}
	active, err := f.store.ResolveActiveAlbumPair(ctx, f.owner, f.bot)
	if err != nil || active.PairKey != "second" {
		t.Fatalf("active target = %+v, %v", active, err)
	}
	// The previous label keeps its row; only its state changed.
	var state string
	if err := f.conn.QueryRow(ctx,
		`SELECT state FROM album_pairs WHERE user_id=$1 AND pair_key='first'`, f.owner).Scan(&state); err != nil || state != "revoked" {
		t.Fatalf("retired row state = %q, %v", state, err)
	}

	list, err := f.store.ListAlbumPairs(ctx, f.owner)
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if list[0].PairKey != "first" || list[1].PairKey != "second" {
		t.Fatalf("list order = %+v", list)
	}
}

func TestAlbumPairPartialUniqueIndexRejectsSecondActiveRow(t *testing.T) {
	f := newAlbumPairFixture(t)
	ctx := t.Context()

	if _, err := f.store.UpsertAlbumPair(ctx, dbstore.AlbumPairRecord{UserID: f.owner, BotID: f.bot, PairKey: "one"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// A direct insert that bypasses the retire step must hit the partial index.
	_, err := f.conn.Exec(ctx,
		`INSERT INTO album_pairs(user_id,bot_id,pair_key,state) VALUES($1,$2,'two','active')`, f.owner, f.bot)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.ConstraintName != "album_pairs_active_bot" {
		t.Fatalf("second active insert error = %v, want album_pairs_active_bot", err)
	}
}

func TestAlbumPairLabelIsUniquePerOwnerAcrossBots(t *testing.T) {
	f := newAlbumPairFixture(t)
	ctx := t.Context()

	if _, err := f.store.UpsertAlbumPair(ctx, dbstore.AlbumPairRecord{UserID: f.owner, BotID: f.bot, PairKey: "shared"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// The same label for another bot moves that one row; it never mints a second.
	moved, err := f.store.UpsertAlbumPair(ctx, dbstore.AlbumPairRecord{UserID: f.owner, BotID: f.other, PairKey: "shared"})
	if err != nil || moved.PairKey != "shared" || moved.BotID != f.other {
		t.Fatalf("moved = %+v, %v", moved, err)
	}
	if moved.ID != "" && moved.CreatedAt.IsZero() {
		t.Fatalf("moved row lost its timestamp: %+v", moved)
	}
	if _, err := f.store.ResolveActiveAlbumPair(ctx, f.owner, f.bot); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatalf("old bot resolve error = %v, want ErrNotFound", err)
	}
}

func TestAlbumPairRevokeKeepsTheRowAndItsKey(t *testing.T) {
	f := newAlbumPairFixture(t)
	ctx := t.Context()

	created, err := f.store.UpsertAlbumPair(ctx, dbstore.AlbumPairRecord{UserID: f.owner, BotID: f.bot, PairKey: "red-jade"})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	revoked, err := f.store.RevokeAlbumPair(ctx, f.owner, "red-jade")
	if err != nil || revoked.State != "revoked" {
		t.Fatalf("revoke = %+v, %v", revoked, err)
	}
	if !revoked.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("revoke changed createdAt: %v -> %v", created.CreatedAt, revoked.CreatedAt)
	}
	repeat, err := f.store.RevokeAlbumPair(ctx, f.owner, "red-jade")
	if err != nil || !repeat.UpdatedAt.Equal(revoked.UpdatedAt) {
		t.Fatalf("repeat revoke = %+v, %v", repeat, err)
	}
	var count int
	if err := f.conn.QueryRow(ctx, `SELECT count(*) FROM album_pairs WHERE user_id=$1`, f.owner).Scan(&count); err != nil || count != 1 {
		t.Fatalf("rows after revoke = %d, %v", count, err)
	}
	if _, err := f.store.RevokeAlbumPair(ctx, f.owner, "absent"); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatalf("unknown label error = %v, want ErrNotFound", err)
	}
	// Naming the same album again reactivates the same row.
	back, err := f.store.UpsertAlbumPair(ctx, dbstore.AlbumPairRecord{UserID: f.owner, BotID: f.bot, PairKey: "red-jade"})
	if err != nil || back.State != "active" || !back.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("reactivate = %+v, %v", back, err)
	}
}

func TestAlbumPairRejectsAForeignBot(t *testing.T) {
	f := newAlbumPairFixture(t)
	_, err := f.store.UpsertAlbumPair(t.Context(), dbstore.AlbumPairRecord{UserID: f.owner, BotID: uuid.NewString(), PairKey: "p"})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("unknown bot error = %v, want 23503", err)
	}
}

func TestAlbumPairRLSHidesForeignTeams(t *testing.T) {
	f := newAlbumPairFixture(t)
	ctx := context.Background()
	if _, err := f.store.UpsertAlbumPair(t.Context(), dbstore.AlbumPairRecord{UserID: f.owner, BotID: f.bot, PairKey: "p"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	foreign := rlsConn(t, f.pool, teamMigrationDSN(t))
	if _, err := foreign.Exec(ctx, "SELECT set_config('memoh.team_id',$1,false)", uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	store := postgresstore.NewWithQueries(dbsqlc.New(foreign))
	if rows, err := store.ListAlbumPairs(ctx, f.owner); err != nil || len(rows) != 0 {
		t.Fatalf("foreign team listed %d rows, %v", len(rows), err)
	}
	if _, err := store.ResolveActiveAlbumPair(ctx, f.owner, f.bot); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatalf("foreign team resolved the pairing: %v", err)
	}
}

// The pairing archive and the write-back queue are joined only by (user, label):
// the commit-time sink resolves the active pairing, then queues the verified
// hash into 0189 with the queue's own idempotency.
func TestAlbumPairQueuesAVerifiedDelivery(t *testing.T) {
	f := newAlbumPairFixture(t)
	ctx := t.Context()
	pairs := albumpairs.NewService(nil, f.store)
	writebacks := albumwriteback.NewService(nil, f.store, nil)

	if _, err := pairs.Upsert(ctx, f.owner, "red-jade", f.bot); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	pair, err := pairs.Resolve(ctx, f.owner, f.bot)
	if err != nil || pair.PairKey != "red-jade" {
		t.Fatalf("Resolve = %+v, %v", pair, err)
	}
	hash := albumHash("verified-product")
	if _, err := writebacks.EnqueueVerified(ctx, f.owner, f.bot, pair.PairKey, albumwriteback.VerifiedItem{
		ContentHash: hash,
		SizeBytes:   4096,
		Mime:        "image/jpeg",
	}); err != nil {
		t.Fatalf("EnqueueVerified: %v", err)
	}
	// A repeated commit of the same product keeps one pending row.
	if _, err := writebacks.EnqueueVerified(ctx, f.owner, f.bot, pair.PairKey, albumwriteback.VerifiedItem{
		ContentHash: hash,
		SizeBytes:   4096,
		Mime:        "image/jpeg",
	}); err != nil {
		t.Fatalf("repeat EnqueueVerified: %v", err)
	}
	pending, err := writebacks.Pending(ctx, f.owner)
	if err != nil || len(pending.Items) != 1 {
		t.Fatalf("pending = %+v, %v", pending.Items, err)
	}
	if pending.Items[0].PairKey != "red-jade" || pending.Items[0].ContentHash != hash {
		t.Fatalf("queued item = %+v", pending.Items[0])
	}
	// Unbinding the pairing keeps the queued item: the product is already in.
	if _, err := pairs.Revoke(ctx, f.owner, "red-jade"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if got, err := writebacks.Pending(ctx, f.owner); err != nil || len(got.Items) != 1 {
		t.Fatalf("unbind dropped the pending item: %+v %v", got.Items, err)
	}
	// A product written and then delivered again returns to pending.
	if _, err := writebacks.MarkWritten(ctx, f.owner, "red-jade", hash); err != nil {
		t.Fatalf("MarkWritten: %v", err)
	}
	if _, err := pairs.Upsert(ctx, f.owner, "red-jade", f.bot); err != nil {
		t.Fatalf("re-activate: %v", err)
	}
	if _, err := writebacks.EnqueueVerified(ctx, f.owner, f.bot, "red-jade", albumwriteback.VerifiedItem{
		ContentHash: hash,
		SizeBytes:   4096,
		Mime:        "image/jpeg",
	}); err != nil {
		t.Fatalf("re-EnqueueVerified: %v", err)
	}
	if got, err := writebacks.Pending(ctx, f.owner); err != nil || len(got.Items) != 1 || got.Items[0].ContentHash != hash {
		t.Fatalf("re-queued item = %+v %v", got.Items, err)
	}
}
