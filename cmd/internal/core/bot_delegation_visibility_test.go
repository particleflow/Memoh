package core

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

type delegationVisibilityQueries struct {
	dbstore.Queries
	created []sqlc.CreateSessionParams
}

func (q *delegationVisibilityQueries) CreateSession(_ context.Context, input sqlc.CreateSessionParams) (sqlc.BotSession, error) {
	q.created = append(q.created, input)
	id, err := db.ParseUUID(uuid.NewString())
	return sqlc.BotSession{ID: id, BotID: input.BotID, CreatedByUserID: input.CreatedByUserID, Type: input.Type, SessionMode: input.SessionMode, RuntimeType: input.RuntimeType, Visibility: input.Visibility, Metadata: input.Metadata}, err
}

func TestDelegationSessionIsInternalWithoutTitleBasedFiltering(t *testing.T) {
	q := &delegationVisibilityQueries{}
	sessions := thread.NewService(nil, q, nil)
	host := &delegationHost{sessions: sessions}
	bot, user := uuid.NewString(), uuid.NewString()
	if _, err := host.CreateSession(t.Context(), botdelegation.Record{Identity: botdelegation.Identity{TargetBotID: bot, UserID: user}}); err != nil {
		t.Fatal(err)
	}
	// An ordinary user chat with exactly the same title stays visible.
	if _, err := sessions.Create(t.Context(), thread.CreateInput{BotID: bot, CreatedByUserID: user, Type: thread.TypeChat, Title: "Delegated task", ChannelType: "web"}); err != nil {
		t.Fatal(err)
	}
	if len(q.created) != 2 || q.created[0].Visibility != string(thread.VisibilityInternal) || q.created[1].Visibility != string(thread.VisibilityUser) {
		t.Fatalf("wrong session visibility: %#v", q.created)
	}
}
