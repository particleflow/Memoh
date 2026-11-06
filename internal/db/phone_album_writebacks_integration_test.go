//go:build integration

package db_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felinics/memoh/internal/albumwriteback"
	memohdb "github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/team"
)

// albumWritebackFixture drives the real queue: migration 0189, the enqueue
// upsert identity, the ordered pending read and the RLS boundary.
type albumWritebackFixture struct {
	pool        *pgxpool.Pool
	conn        *pgxpool.Conn
	store       *postgresstore.Store
	owner       string
	bot         string
	service     *albumwriteback.Service
	knownHashes []string
}

type albumFixtureAssets struct{ owner string }

func (a albumFixtureAssets) Stat(_ context.Context, botID, contentHash string) (media.Asset, error) {
	return media.Asset{ContentHash: contentHash, BotID: botID, Mime: "image/jpeg", SizeBytes: 7, StorageKey: contentHash[:2] + "/" + contentHash + ".jpg"}, nil
}

func newAlbumWritebackFixture(t *testing.T) albumWritebackFixture {
	t.Helper()
	pool := freshMigratedDB(t)
	conn, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Release)
	f := albumWritebackFixture{
		pool:  pool,
		conn:  conn,
		store: postgresstore.NewWithQueries(dbsqlc.New(conn)),
		owner: uuid.NewString(),
		bot:   uuid.NewString(),
	}
	f.exec(t, "SELECT set_config('memoh.team_id',$1,false)", team.DefaultTeamID)
	f.exec(t, "INSERT INTO users(id,username) VALUES($1,'album-test')", f.owner)
	f.exec(t, "INSERT INTO team_members(user_id) VALUES($1)", f.owner)
	f.exec(t, "INSERT INTO bots(id,owner_user_id,name) VALUES($1,$2,'album-test')", f.bot, f.owner)
	f.service = albumwriteback.NewService(nil, f.store, albumFixtureAssets{owner: f.owner})
	return f
}

func (f albumWritebackFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.conn.Exec(t.Context(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func albumHash(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

func TestPhoneAlbumWritebackEnqueueIsIdempotentAndReopensWritten(t *testing.T) {
	f := newAlbumWritebackFixture(t)
	hash := albumHash("album-a")
	in := albumwriteback.EnqueueInput{PairKey: "processed-default", ContentHashes: []string{hash}, Name: "结果.jpg"}

	first, err := f.service.Enqueue(t.Context(), f.owner, f.bot, in)
	if err != nil || len(first.Enqueued) != 1 {
		t.Fatalf("Enqueue = %+v, %v", first, err)
	}
	// The same identity enqueued again stays one row.
	if _, err := f.service.Enqueue(t.Context(), f.owner, f.bot, in); err != nil {
		t.Fatalf("repeat Enqueue: %v", err)
	}
	pending, err := f.service.Pending(t.Context(), f.owner)
	if err != nil || len(pending.Items) != 1 {
		t.Fatalf("pending = %+v, %v", pending.Items, err)
	}
	item := pending.Items[0]
	if item.ContentHash != hash || item.PairKey != "processed-default" || item.Name != "结果.jpg" ||
		item.Mime != "image/jpeg" || item.SizeBytes == nil || *item.SizeBytes != 7 {
		t.Fatalf("item = %+v", item)
	}

	if _, err := f.service.MarkWritten(t.Context(), f.owner, "processed-default", hash); err != nil {
		t.Fatalf("MarkWritten: %v", err)
	}
	if _, err := f.service.MarkWritten(t.Context(), f.owner, "processed-default", hash); err != nil {
		t.Fatalf("repeat MarkWritten: %v", err)
	}
	if got, err := f.service.Pending(t.Context(), f.owner); err != nil || len(got.Items) != 0 {
		t.Fatalf("written item stayed pending: %+v %v", got.Items, err)
	}
	// Written again: the item returns to the queue, and the queue order puts it last.
	if _, err := f.service.Enqueue(t.Context(), f.owner, f.bot, albumwriteback.EnqueueInput{PairKey: "processed-default", ContentHashes: []string{hash}}); err != nil {
		t.Fatalf("re-Enqueue: %v", err)
	}
	if got, err := f.service.Pending(t.Context(), f.owner); err != nil || len(got.Items) != 1 || got.Items[0].ContentHash != hash {
		t.Fatalf("re-enqueued item = %+v %v", got.Items, err)
	}
	var state string
	if err := f.conn.QueryRow(t.Context(),
		`SELECT state FROM phone_album_writebacks WHERE user_id=$1 AND pair_key='processed-default' AND content_hash=$2`,
		f.owner, hash).Scan(&state); err != nil || state != "pending" {
		t.Fatalf("stored state = %q, %v", state, err)
	}
}

func TestPhoneAlbumWritebackPendingOrderAndOwnerScope(t *testing.T) {
	f := newAlbumWritebackFixture(t)
	one, two := albumHash("order-one"), albumHash("order-two")
	for _, hash := range []string{two, one} {
		if _, err := f.service.Enqueue(t.Context(), f.owner, f.bot, albumwriteback.EnqueueInput{PairKey: "p", ContentHashes: []string{hash}}); err != nil {
			t.Fatalf("Enqueue %s: %v", hash, err)
		}
	}
	pending, err := f.service.Pending(t.Context(), f.owner)
	if err != nil || len(pending.Items) != 2 {
		t.Fatalf("pending = %+v, %v", pending.Items, err)
	}
	if pending.Items[0].ContentHash != two || pending.Items[1].ContentHash != one {
		t.Fatalf("pending order = %+v", pending.Items)
	}
	// Another user sees nothing and cannot mark this owner's item.
	other := uuid.NewString()
	if got, err := f.service.Pending(t.Context(), other); err != nil || len(got.Items) != 0 {
		t.Fatalf("other owner pending = %+v %v", got.Items, err)
	}
	if _, err := f.service.MarkWritten(t.Context(), other, "p", two); !errors.Is(err, albumwriteback.ErrNotFound) {
		t.Fatalf("other owner MarkWritten error = %v, want ErrNotFound", err)
	}
	if _, err := f.service.MarkWritten(t.Context(), f.owner, "p", albumHash("absent")); !errors.Is(err, albumwriteback.ErrNotFound) {
		t.Fatalf("absent item MarkWritten error = %v, want ErrNotFound", err)
	}
}

func TestPhoneAlbumWritebackRLSHidesForeignTeams(t *testing.T) {
	f := newAlbumWritebackFixture(t)
	hash := albumHash("rls")
	if _, err := f.service.Enqueue(t.Context(), f.owner, f.bot, albumwriteback.EnqueueInput{PairKey: "p", ContentHashes: []string{hash}}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	foreign := rlsConn(t, f.pool, teamMigrationDSN(t))
	if _, err := foreign.Exec(context.Background(), "SELECT set_config('memoh.team_id',$1,false)", uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	store := postgresstore.NewWithQueries(dbsqlc.New(foreign))
	if rows, err := store.ListPendingPhoneAlbumWritebacks(context.Background(), f.owner); err != nil || len(rows) != 0 {
		t.Fatalf("foreign team listed %d rows, %v", len(rows), err)
	}
	if _, err := store.MarkPhoneAlbumWritebackWritten(context.Background(), f.owner, "p", hash); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatalf("foreign team marked the item: %v", err)
	}
}
