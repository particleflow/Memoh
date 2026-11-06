//go:build integration

package db_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	memohdb "github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/team"
	"github.com/google/uuid"
)

func TestPiBatchHistoryPriorEvidenceIsScopedAndLatest(t *testing.T) {
	ctx := t.Context()
	pool := freshMigratedDB(t)
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err = conn.Exec(ctx, "SELECT set_config('memoh.team_id',$1,false)", team.DefaultTeamID); err != nil {
		t.Fatal(err)
	}
	user, bot, session, target := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO users(id,username) VALUES($1,'pi-history-test')", []any{user}},
		{"INSERT INTO team_members(user_id) VALUES($1)", []any{user}},
		{"INSERT INTO bots(id,owner_user_id,name) VALUES($1,$2,'pi-history-test')", []any{bot, user}},
		{"INSERT INTO bot_sessions(id,bot_id) VALUES($1,$2)", []any{session, bot}},
	} {
		if _, err = conn.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	store := postgresstore.NewWithQueries(dbsqlc.New(conn))
	sid := strings.Repeat("a", 64)
	raw, _ := json.Marshal(map[string]any{"batch": map[string]any{"progress": map[string]string{"pi_session_id": sid}}})
	input := dbstore.ComputerOperationRecord{ID: uuid.NewString(), BotID: bot, SessionID: session, UserID: user, TargetID: target, RunID: "first", Fingerprint: "first", TargetFingerprint: "registration", Details: raw}
	first, won, err := store.ClaimComputerOperation(ctx, input)
	if err != nil || !won {
		t.Fatal(err)
	}
	if _, err = store.PreviousComputerPiOperation(ctx, first, sid); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatalf("current operation counted as prior: %v", err)
	}
	first.State = "completed"
	first, err = store.SaveComputerOperation(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	input.ID = uuid.NewString()
	input.RunID = "second"
	input.Fingerprint = "second"
	second, won, err := store.ClaimComputerOperation(ctx, input)
	if err != nil || !won {
		t.Fatal(err)
	}
	previous, err := store.PreviousComputerPiOperation(ctx, second, sid)
	if err != nil || previous.ID != first.ID {
		t.Fatalf("prior: %s %v", previous.ID, err)
	}
	for _, field := range []string{"bot", "session", "user", "target", "pi"} {
		foreign := second
		otherSID := sid
		switch field {
		case "bot":
			foreign.BotID = uuid.NewString()
		case "session":
			foreign.SessionID = uuid.NewString()
		case "user":
			foreign.UserID = uuid.NewString()
		case "target":
			foreign.TargetID = uuid.NewString()
		case "pi":
			otherSID = strings.Repeat("b", 64)
		}
		if _, err = store.PreviousComputerPiOperation(ctx, foreign, otherSID); !errors.Is(err, memohdb.ErrNotFound) {
			t.Fatalf("scope %s leaked: %v", field, err)
		}
	}
	second.State = "not_started"
	second, err = store.SaveComputerOperation(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	input.ID = uuid.NewString()
	input.RunID = "third"
	input.Fingerprint = "third"
	third, won, err := store.ClaimComputerOperation(ctx, input)
	if err != nil || !won {
		t.Fatal(err)
	}
	restarted := postgresstore.NewWithQueries(dbsqlc.New(conn))
	previous, err = restarted.PreviousComputerPiOperation(ctx, third, sid)
	if err != nil || previous.ID != second.ID {
		t.Fatal("unproven latest operation skipped", err)
	}
	if _, err = conn.Exec(ctx, "SELECT set_config('memoh.team_id',$1,false)", uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err = store.PreviousComputerPiOperation(ctx, third, sid); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatal("foreign team history exposed", err)
	}
}
