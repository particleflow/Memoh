package computertask

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/bots"
	thread "github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/sessiontask"
	"github.com/felinics/memoh/internal/workdir"
)

// NewPostgresDeliveryService reuses existing authorization services against one
// transaction snapshot. It does not mint identity or expose a network listener.
func NewPostgresDeliveryService(store *postgresstore.Store, assets DeliveryAssets) *DeliveryService {
	return NewDeliveryService(func(ctx context.Context, fn func(DeliveryTransaction) error) error {
		if store == nil || store.Pool() == nil {
			return ErrDeliveryBinding // No non-transactional fallback.
		}
		tx, err := store.Pool().BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
		if err != nil {
			return err
		}
		defer func() {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_ = tx.Rollback(cleanup)
		}()
		q := store.SQLC().WithTx(tx)
		txStore := postgresstore.NewWithQueries(q)
		queries := postgresstore.NewQueries(q)
		permissions := &deliveryPermissions{bots: bots.NewService(nil, queries), accounts: accounts.NewService(nil, txStore)}
		targets := agenvo.NewService(txStore, permissions, nil)
		// Get does not use cursor encryption or publish events. Both readers are
		// backed by this transaction; no device or Agent call is made.
		tasks := sessiontask.NewService(txStore, thread.NewService(nil, queries, nil), workdir.NewService(txStore, nil), targets, "")
		authorize := func(ctx context.Context, id DeliveryIdentity) (agenvo.Target, error) {
			b := id.Binding
			for _, permission := range []string{bots.PermissionChat, bots.PermissionWorkspaceRead, bots.PermissionWorkspaceWrite, bots.PermissionWorkspaceExec} {
				allowed, err := permissions.HasBotPermission(ctx, b.BotID, b.UserID, permission)
				if err != nil {
					return agenvo.Target{}, err
				}
				if !allowed {
					return agenvo.Target{}, ErrDenied
				}
			}
			item, err := tasks.Get(ctx, b.BotID, b.UserID, b.SessionID, sessiontask.ComputerOperation, id.OperationID)
			if err != nil || item.SessionID != b.SessionID || item.TargetID != b.TargetID {
				return agenvo.Target{}, ErrDenied
			}
			// Historical task metadata can survive retirement, but delivery must
			// still pass the live target gate and retain its public denial.
			target, err := targets.ResolveTarget(ctx, agenvo.Scope{BotID: b.BotID, UserID: b.UserID, TargetID: b.TargetID})
			if err != nil {
				return agenvo.Target{}, ErrDenied
			}
			return target, nil
		}
		if err = fn(DeliveryTransaction{Store: txStore, Authorize: authorize}); err != nil {
			return deliveryTransactionError(err, false)
		}
		return deliveryTransactionError(tx.Commit(ctx), true)
	}, assets)
}

func deliveryTransactionError(err error, committing bool) error {
	if err == nil {
		return nil
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && (pg.Code == "40001" || pg.Code == "40P01") {
		return ErrDeliveryRetry // PostgreSQL confirms transaction abort.
	}
	if committing && !errors.Is(err, pgx.ErrTxCommitRollback) {
		return fmt.Errorf("%w: %w", db.ErrCommitOutcomeUnknown, err)
	}
	return err
}

type deliveryPermissions struct {
	bots     *bots.Service
	accounts *accounts.Service
}

func (p *deliveryPermissions) HasBotPermission(ctx context.Context, bot, user, permission string) (bool, error) {
	// IsAdmin alone returns false for inactive users; owner bypass must not
	// thereby permit a deactivated account to commit or query a delivery.
	if err := p.accounts.ValidateSession(ctx, user); err != nil {
		if errors.Is(err, accounts.ErrInactiveAccount) {
			return false, ErrDenied
		}
		return false, err
	}
	admin, err := p.accounts.IsAdmin(ctx, user)
	if err != nil {
		return false, err
	}
	permissions, err := p.bots.ResolveUserPermissions(ctx, bot, user, admin)
	return err == nil && bots.HasPermission(permissions, permission), err
}
