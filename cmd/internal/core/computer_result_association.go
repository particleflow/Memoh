package core

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/chat/event"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/runtimefence"
)

type computerResultAnchorStore interface {
	GetSessionByID(context.Context, pgtype.UUID) (sqlc.BotSession, error)
	GetSessionRun(context.Context, pgtype.UUID) (sqlc.SessionRun, error)
	GetHistoryTurnByID(context.Context, sqlc.GetHistoryTurnByIDParams) (sqlc.GetHistoryTurnByIDRow, error)
	GetMessageByIDBySession(context.Context, sqlc.GetMessageByIDBySessionParams) (sqlc.GetMessageByIDBySessionRow, error)
}
type computerResultAssetLinker interface {
	LinkAssets(context.Context, string, []messagepkg.AssetRef) error
}

func injectComputerResultAssociator(source *computertask.Source, store *postgresstore.Store, messages *messagepkg.DBService, accounts *accounts.Service, hub *event.Hub, botService *bots.Service) {
	permissions := &applicationBotPermissionChecker{bots: botService, accounts: accounts}
	source.SetResultAssociator(func(ctx context.Context, r dbstore.ComputerOperationRecord, refs []computertask.DeliveryRequest) error {
		if err := accounts.ValidateSession(ctx, r.UserID); err != nil {
			return computertask.ErrDenied
		}
		allowed, err := permissions.HasBotPermission(ctx, r.BotID, r.UserID, bots.PermissionChat)
		if err != nil || !allowed {
			return computertask.ErrDenied
		}
		if err := associateComputerResult(ctx, store, messages, r, refs); err != nil {
			return err
		}
		event.InvalidateSession(hub, r.BotID, r.SessionID)
		return nil
	})
}

// The stable assistant anchor belongs to the operation's original run, not the
// latest message or the current observer run. LinkAssets uses its existing
// fenced transaction and (message, hash) upsert; no send or new message occurs.
func associateComputerResult(ctx context.Context, store computerResultAnchorStore, messages computerResultAssetLinker, r dbstore.ComputerOperationRecord, refs []computertask.DeliveryRequest) error {
	fence, ok := runtimefence.FromContext(ctx)
	if !ok || fence.Token <= 0 || fence.BotID != r.BotID || fence.SessionID != r.SessionID {
		return computertask.ErrDenied
	}
	session, err := store.GetSessionByID(ctx, db.ParseUUIDOrEmpty(r.SessionID))
	if err != nil {
		return computertask.ErrDenied
	}
	if session.ID.String() != r.SessionID || session.BotID.String() != r.BotID || session.DeletedAt.Valid || session.CreatedByUserID.String() != r.UserID {
		return computertask.ErrDenied
	}
	run, err := store.GetSessionRun(ctx, db.ParseUUIDOrEmpty(r.RunID))
	if err != nil {
		return computertask.ErrDenied
	}
	if run.RunID.String() != r.RunID || run.BotID != session.BotID || run.SessionID != session.ID || run.TeamID != session.TeamID || run.ErrorCode.String == "history_reset" || !run.TurnID.Valid {
		return computertask.ErrDenied
	}
	var input struct {
		Resume struct {
			Version int    `json:"version"`
			UserID  string `json:"user_id"`
		} `json:"resume"`
	}
	if json.Unmarshal(run.InputJson, &input) != nil || input.Resume.Version != 1 || input.Resume.UserID != r.UserID {
		return computertask.ErrDenied
	}
	turn, err := store.GetHistoryTurnByID(ctx, sqlc.GetHistoryTurnByIDParams{SessionID: session.ID, OldTurnID: run.TurnID})
	if err != nil {
		return computertask.ErrDenied
	}
	if turn.ID != run.TurnID || turn.BotID != session.BotID || turn.SessionID != session.ID || turn.SupersededByTurnID.Valid || !turn.AssistantMessageID.Valid {
		return computertask.ErrDenied
	}
	anchor, err := store.GetMessageByIDBySession(ctx, sqlc.GetMessageByIDBySessionParams{SessionID: session.ID, MessageID: turn.AssistantMessageID})
	if err != nil {
		return computertask.ErrDenied
	}
	if anchor.BotID != session.BotID || anchor.ID != turn.AssistantMessageID || anchor.SessionID != session.ID || anchor.Role != "assistant" {
		return computertask.ErrDenied
	}
	assets := make([]messagepkg.AssetRef, 0, len(refs))
	seen := map[string]bool{}
	for _, ref := range refs {
		if seen[ref.ContentHash] {
			continue
		}
		seen[ref.ContentHash] = true
		assets = append(assets, messagepkg.AssetRef{ContentHash: ref.ContentHash, Role: "attachment", Ordinal: len(assets), Mime: ref.Mime, SizeBytes: ref.SizeBytes, Metadata: map[string]any{"computer_operation_id": r.ID, "computer_run_id": r.RunID}})
	}
	if err := messages.LinkAssets(ctx, anchor.ID.String(), assets); err != nil {
		return computertask.ErrDenied
	}
	return nil
}
