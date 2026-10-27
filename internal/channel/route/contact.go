package route

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

var ErrContactUnavailable = errors.New("private chat route unavailable")

type contactTransactions interface {
	SupportsTransactions() bool
	InTx(context.Context, func(dbstore.Queries) error) error
}

type routeSessionStager interface {
	StageRouteSession(context.Context, dbstore.Queries, thread.CreateInput) (thread.Thread, func(), error)
}

// EnsurePrivateChat resolves one server-owned Web contact per account and Bot.
// This does not adopt, merge, hide or repoint any historical conversation.
func (c *ThreadCoordinator) EnsurePrivateChat(ctx context.Context, bot, user string) (thread.Thread, error) {
	bid, err := uuid.Parse(bot)
	if err != nil || bid == uuid.Nil {
		return thread.Thread{}, ErrContactUnavailable
	}
	uid, err := uuid.Parse(user)
	if err != nil || uid == uuid.Nil {
		return thread.Thread{}, ErrContactUnavailable
	}
	routes, ok := c.routes.(*DBService)
	if !ok {
		return thread.Thread{}, ErrContactUnavailable
	}
	tx, ok := routes.queries.(contactTransactions)
	stager, canStage := c.threads.(routeSessionStager)
	if !ok || !tx.SupportsTransactions() || !canStage {
		return thread.Thread{}, ErrContactUnavailable
	}
	var result thread.Thread
	var committed func()
	err = tx.InTx(ctx, func(q dbstore.Queries) error {
		// Upsert locks the unique route until both creation and activation commit.
		r, err := q.EnsurePrivateChatRoute(ctx, sqlc.EnsurePrivateChatRouteParams{
			BotID: pgtype.UUID{Bytes: bid, Valid: true}, ConversationID: "contact:" + uid.String(),
			ReplyTarget: pgtype.Text{String: bid.String(), Valid: true},
		})
		if err != nil {
			return err
		}
		if r.ActiveSessionID.Valid {
			stored, readErr := q.GetSessionByID(ctx, r.ActiveSessionID)
			if readErr == nil {
				// Validate the full original binding, never recover by a title/name.
				if stored.BotID != r.BotID || stored.CreatedByUserID != (pgtype.UUID{Bytes: uid, Valid: true}) || stored.RouteID != r.ID || stored.SessionMode != thread.TypeChat || stored.RuntimeType != thread.RuntimeModel || stored.Visibility != string(thread.VisibilityUser) || stored.ParentSessionID.Valid {
					return ErrContactUnavailable
				}
				// The transaction-local service reads the same committed descriptor.
				result, readErr = thread.NewService(c.logger, q, nil).Get(ctx, stored.ID.String())
				return readErr
			}
			if !errors.Is(readErr, pgx.ErrNoRows) {
				return readErr
			}
		}
		result, committed, err = stager.StageRouteSession(ctx, q, thread.CreateInput{
			BotID: bid.String(), RouteID: r.ID.String(), CreatedByUserID: uid.String(),
			ChannelType: "web", Type: thread.TypeChat, Visibility: thread.VisibilityUser,
		})
		if err != nil {
			return err
		}
		id, err := uuid.Parse(result.ID)
		if err != nil {
			return fmt.Errorf("invalid created contact session: %w", err)
		}
		return q.SetRouteActiveSession(ctx, sqlc.SetRouteActiveSessionParams{ID: r.ID, ActiveSessionID: pgtype.UUID{Bytes: id, Valid: true}})
	})
	if err != nil {
		return thread.Thread{}, err
	}
	if committed != nil {
		committed()
	}
	return result, nil
}
