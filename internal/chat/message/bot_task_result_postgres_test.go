package message

import (
	"testing"

	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
)

func TestPostgresBotTaskResultOwnTurnAndModelHistory(t *testing.T) {
	ctx := t.Context()
	tx := beginPostgresMessageTestTx(t, ctx)
	setupPostgresMessageTestFixtures(t, ctx, tx)
	svc := NewService(nil, postgresstore.NewQueries(dbsqlc.New(tx)))
	// A result has no earlier user request to bind to. It leads a distinct
	// history turn, while old readers still recognize its internal source.
	input, err := svc.Persist(ctx, PersistInput{BotID: postgresMessageTestBotID, SessionID: postgresMessageTestSessionID, Role: "user", Content: []byte(`{"role":"user","content":"machine result data"}`), Metadata: map[string]any{"message_source": "internal_feedback", "internal_input_kind": "bot_task_result", "delegation_id": "task-one"}})
	if err != nil {
		t.Fatal(err)
	}
	answer, err := svc.Persist(ctx, PersistInput{BotID: postgresMessageTestBotID, SessionID: postgresMessageTestSessionID, Role: "assistant", Content: []byte(`{"role":"assistant","content":"The task finished."}`), TurnRequestMessageID: input.ID})
	if err != nil {
		t.Fatal(err)
	}
	if input.TurnID == "" || answer.TurnID != input.TurnID {
		t.Fatalf("lost own result turn: input=%+v answer=%+v", input, answer)
	}
	history, err := svc.ListBySession(ctx, postgresMessageTestSessionID)
	if err != nil || len(history) != 2 || history[0].Metadata["delegation_id"] != "task-one" {
		t.Fatalf("model history lost: %+v %v", history, err)
	}
}
