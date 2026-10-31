package computertask

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/team"
)

func TestBatchReaderPostgresPersistenceCAS(t *testing.T) {
	dsn := os.Getenv("MEMOH_AGENVO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated migrated PostgreSQL test database")
	}
	ctx := t.Context()
	pool, err := db.OpenPostgresDSN(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT set_config('memoh.team_id',$1,true)", team.DefaultTeamID); err != nil {
		t.Fatal(err)
	}
	user, bot, session := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, query := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO users(id) VALUES($1)", []any{user}},
		{"INSERT INTO team_members(user_id) VALUES($1)", []any{user}},
		{"INSERT INTO bots(id,owner_user_id,name) VALUES($1,$2,'synthetic-reader')", []any{bot, user}},
		{"INSERT INTO bot_sessions(id,bot_id) VALUES($1,$2)", []any{session, bot}},
	} {
		if _, err := tx.Exec(ctx, query.sql, query.args...); err != nil {
			t.Fatal(err)
		}
	}
	store := postgresstore.NewWithQueries(dbsqlc.New(tx))
	progress := agenvo.BatchProgress{Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32), RequestHash: strings.Repeat("c", 64), PiSessionID: strings.Repeat("d", 64), Phase: "batch_submitted", WorkspaceID: "original", PaneID: "pi", TerminalID: "pi-original", ControlPane: "control", ControlTerminal: "control-original"}
	d := Details{Kind: "herdr_pi_task", Batch: &BatchDetails{Progress: progress}}
	raw, _ := json.Marshal(d)
	record, won, err := store.ClaimComputerOperation(ctx, dbstore.ComputerOperationRecord{ID: uuid.NewString(), BotID: bot, SessionID: session, UserID: user, TargetID: uuid.NewString(), RunID: "synthetic-reader", Fingerprint: "synthetic-request", TargetFingerprint: "synthetic-registration", Details: raw})
	if err != nil || !won {
		t.Fatalf("claim: %v %v", won, err)
	}
	stale := record
	d.Batch.RecoveryReader = &agenvo.BatchRecoveryReader{RequestID: uuid.NewString(), Phase: "creating"}
	record.Details, _ = json.Marshal(d)
	record, err = store.SaveComputerOperation(ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveComputerOperation(ctx, stale); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("stale CAS accepted: %v", err)
	}
	restarted := postgresstore.NewWithQueries(dbsqlc.New(tx))
	loaded, err := restarted.ReadComputerOperation(ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	var persisted Details
	if err := json.Unmarshal(loaded.Details, &persisted); err != nil || !reflect.DeepEqual(persisted.Batch.RecoveryReader, d.Batch.RecoveryReader) || !reflect.DeepEqual(persisted.Batch.Progress, progress) {
		t.Fatal("creating reservation/identity did not survive store reconstruction")
	}
	reader := persisted.Batch.RecoveryReader
	reader.Phase, reader.WorkspaceID, reader.PaneID, reader.TerminalID = "ready", "recovery-workspace", "recovery-pane", "recovery-terminal"
	loaded.Details, _ = json.Marshal(persisted)
	ready, err := restarted.SaveComputerOperation(ctx, loaded)
	if err != nil {
		t.Fatal(err)
	}
	ready, err = store.ReadComputerOperation(ctx, ready)
	if err != nil {
		t.Fatal(err)
	}
	var final Details
	if err := json.Unmarshal(ready.Details, &final); err != nil || !reflect.DeepEqual(final.Batch.RecoveryReader, reader) || !reflect.DeepEqual(final.Batch.Progress, progress) || ready.Revision != stale.Revision+2 || ready.State != stale.State {
		t.Fatal("ready reader changed execution identity/state or did not use original CAS")
	}
}
