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

var errContactChanged = errors.New("private chat binding changed while locking")

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
	for attempt := 0; attempt < 3; attempt++ {
		committed = nil
		err = tx.InTx(ctx, func(q dbstore.Queries) error {
			// Match the native Bot -> Session -> Route lock order. This parent lock
			// excludes Bot deletion without serializing unrelated account contacts.
			if _, err := q.LockBotForSessionWrite(ctx, pgtype.UUID{Bytes: bid, Valid: true}); err != nil {
				return err
			}
			previous, lookupErr := q.FindChatRoute(ctx, sqlc.FindChatRouteParams{
				BotID: pgtype.UUID{Bytes: bid, Valid: true}, Platform: "web", ConversationID: "contact:" + uid.String(),
			})
			if lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows) {
				return lookupErr
			}
			var stored sqlc.BotSession
			if lookupErr == nil {
				// An empty persisted pointer is not evidence that creation is safe.
				// Normal deletion is a tombstone; lock that row even when deleted.
				if !previous.ActiveSessionID.Valid {
					return ErrContactUnavailable
				}
				var lockErr error
				stored, lockErr = q.LockSessionForContact(ctx, sqlc.LockSessionForContactParams{
					SessionID: previous.ActiveSessionID, BotID: pgtype.UUID{Bytes: bid, Valid: true},
				})
				if lockErr != nil {
					if errors.Is(lockErr, pgx.ErrNoRows) {
						return ErrContactUnavailable
					}
					return lockErr
				}
				// Check even tombstones: a foreign deleted row cannot authorize
				// replacing this account's contact pointer.
				if stored.BotID != previous.BotID || stored.CreatedByUserID != (pgtype.UUID{Bytes: uid, Valid: true}) || stored.RouteID != previous.ID || (stored.ChannelType.String != "local" && stored.ChannelType.String != "") || stored.Visibility != string(thread.VisibilityUser) || stored.ParentSessionID.Valid {
					return ErrContactUnavailable
				}
			}
			// Only now lock the unique route. A concurrent first creator may have
			// won while our initial lookup saw no row; roll back and read its session
			// in lock order instead of taking a Session lock while holding Route.
			r, err := q.EnsurePrivateChatRoute(ctx, sqlc.EnsurePrivateChatRouteParams{
				BotID: pgtype.UUID{Bytes: bid, Valid: true}, ConversationID: "contact:" + uid.String(),
				ReplyTarget: pgtype.Text{String: bid.String(), Valid: true},
			})
			if err != nil {
				return err
			}
			if r.Platform != "web" || r.ConversationType.String != "private" || r.ChannelConfigID.Valid || r.ThreadID.String != "" {
				return ErrContactUnavailable
			}
			if (lookupErr == nil && (r.ID != previous.ID || r.ActiveSessionID != previous.ActiveSessionID)) || (lookupErr != nil && r.ActiveSessionID.Valid) {
				return errContactChanged
			}
			if r.ActiveSessionID.Valid && !stored.DeletedAt.Valid {
				// Runtime/mode are mutable preferences, not contact identity.
				// The HTTP boundary applies the current descriptor's read permission.
				var readErr error
				result, readErr = thread.NewService(c.logger, q, nil).Get(ctx, stored.ID.String())
				return readErr
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
		if !errors.Is(err, errContactChanged) {
			break
		}
	}
	if errors.Is(err, errContactChanged) {
		return thread.Thread{}, ErrContactUnavailable
	}
	if err != nil {
		// In particular, an unknown COMMIT is never retried here. A later
		// request reads the same durable key and does not replay its hook.
		return thread.Thread{}, err
	}
	if committed != nil {
		committed()
	}
	return result, nil
}
