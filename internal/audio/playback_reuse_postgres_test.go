//go:build integration

package audio_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	audiopkg "github.com/felinics/memoh/internal/audio"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/dbtest"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
)

const speechCacheDSNEnv = "TEST_POSTGRES_DSN"

// A two-sentence reply. Escapes keep the fixture byte-identical regardless of
// the editor or shell encoding.
const (
	speechFirst  = "\u7b2c\u4e00\u53e5\u8bdd\u3002"
	speechSecond = "\u7b2c\u4e8c\u53e5\u8bdd\u3002"
)

// speechCacheFixture seeds the minimum rows the cache foreign keys require.
func speechCacheFixture(t *testing.T, pool *pgxpool.Pool) (string, string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	user, botID, providerID, modelID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec("INSERT INTO users(id,username)VALUES($1::uuid,$1::text)", user)
	exec("INSERT INTO team_members(user_id)VALUES($1)", user)
	exec("INSERT INTO bots(id,owner_user_id,name)VALUES($1::uuid,$2,$1::text)", botID, user)
	exec("INSERT INTO providers(id,name,client_type,enable,config)VALUES($1::uuid,$1::text,'openai-speech',true,'{}'::jsonb)", providerID)
	exec("INSERT INTO models(id,provider_id,model_id,name,type,enable,config)VALUES($1::uuid,$2::uuid,'speech-fixture','speech-fixture','speech',true,'{}'::jsonb)", modelID, providerID)
	return botID, modelID
}

func speechCachePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv(speechCacheDSNEnv)
	if dsn == "" {
		t.Skip(speechCacheDSNEnv + " is not set")
	}
	if err := dbtest.MigratePostgresUp(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// The connection must carry the team GUC, exactly like the server pool.
	pool, err := db.OpenPostgresDSN(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestSpeechPlaybackCachePersistsAcrossStores is the durability proof: a store
// built from a brand-new pool and query set - a new process, in effect - serves
// the audio recorded by the previous one.
func TestSpeechPlaybackCachePersistsAcrossStores(t *testing.T) {
	pool := speechCachePool(t)
	botID, modelID := speechCacheFixture(t, pool)
	key := audiopkg.PlaybackReusePrefixKey(botID, modelID, "voice-identity", speechFirst).WithUtterance(speechFirst + speechSecond)
	audio := []byte("synthetic-audio-bytes")
	root := t.TempDir()

	first := audiopkg.NewPlaybackReuseStore(postgresstore.NewQueries(dbsqlc.New(pool)), root)
	if err := first.Store(context.Background(), key, audio, "audio/mpeg", ""); err != nil {
		t.Fatal(err)
	}

	// A second store over a different pool is a different process: nothing but
	// the database and the data root can carry the entry across.
	other, err := db.OpenPostgresDSN(context.Background(), os.Getenv(speechCacheDSNEnv))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.Close)
	second := audiopkg.NewPlaybackReuseStore(postgresstore.NewQueries(dbsqlc.New(other)), root)
	if second == nil || second.DataRoot() != first.DataRoot() {
		t.Fatal("second store does not share the data root")
	}
	match, ok := key.Match(speechFirst+speechSecond, second.Candidates(context.Background(), key))
	if !ok || string(match.Audio) != string(audio) {
		t.Fatalf("restart lost the entry: ok=%v audio=%q", ok, match.Audio)
	}
}

// TestSpeechPlaybackCacheRefusesExpiredRows proves the retention window is
// enforced by reads, not only by the reaper.
func TestSpeechPlaybackCacheRefusesExpiredRows(t *testing.T) {
	pool := speechCachePool(t)
	botID, modelID := speechCacheFixture(t, pool)
	store := audiopkg.NewPlaybackReuseStore(postgresstore.NewQueries(dbsqlc.New(pool)), t.TempDir())
	key := audiopkg.PlaybackReusePrefixKey(botID, modelID, "voice-identity", speechFirst).WithUtterance(speechFirst)
	if err := store.Store(context.Background(), key, []byte("bytes"), "audio/mpeg", ""); err != nil {
		t.Fatal(err)
	}
	if len(store.Candidates(context.Background(), key)) != 1 {
		t.Fatal("a fresh entry was not readable")
	}
	conn, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(context.Background(), "UPDATE speech_playback_cache SET expires_at = now() - interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	if got := store.Candidates(context.Background(), key); len(got) != 0 {
		t.Fatalf("an expired entry was served: %d", len(got))
	}
	// The reaper then reclaims both the row and the audio file.
	if err := store.Reap(context.Background()); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := conn.QueryRow(context.Background(), "SELECT count(*) FROM speech_playback_cache").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("reaper left %d expired rows", rows)
	}
}
