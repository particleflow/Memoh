package handlers

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/mcp"
)

type oauthConnectionScopeQueries struct {
	dbstore.Queries
	connection sqlc.McpConnection
}

func (q *oauthConnectionScopeQueries) GetMCPConnectionByID(_ context.Context, arg sqlc.GetMCPConnectionByIDParams) (sqlc.McpConnection, error) {
	if arg.ID != q.connection.ID || arg.BotID != q.connection.BotID {
		return sqlc.McpConnection{}, pgx.ErrNoRows
	}
	return q.connection, nil
}

func TestMCPOAuthRequiresConnectionBelongingToRequestedBot(t *testing.T) {
	const bot = "11111111-1111-4111-8111-111111111111"
	const other = "22222222-2222-4222-8222-222222222222"
	const connection = "33333333-3333-4333-8333-333333333333"
	botID, err := db.ParseUUID(bot)
	if err != nil {
		t.Fatal(err)
	}
	id, err := db.ParseUUID(connection)
	if err != nil {
		t.Fatal(err)
	}
	queries := &oauthConnectionScopeQueries{connection: sqlc.McpConnection{ID: id, BotID: botID, Type: "http", Config: []byte(`{"url":"https://synthetic.invalid/mcp"}`)}}
	h := NewMCPOAuthHandler(nil, mcp.NewConnectionService(nil, queries), nil, nil)
	if err := h.requireOAuthConnection(t.Context(), bot, connection); err != nil {
		t.Fatal(err)
	}
	if err := h.requireOAuthConnection(t.Context(), other, connection); err == nil {
		t.Fatal("Bot access incorrectly granted access to another Bot's OAuth connection")
	}
}
