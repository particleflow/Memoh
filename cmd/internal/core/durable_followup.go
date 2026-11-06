package core

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"time"

	"go.uber.org/fx"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/agent/application"
	runtime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/agent/runtime/session/ledger"
	"github.com/felinics/memoh/internal/agent/turn"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/channel/route"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/config"
	postgres "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/followupqueue"
)

type durableFollowUpPayload struct {
	Command *turn.StartTurnCommand `json:"command"`
}

func httpFollowUpCommand(key runtime.Key, item runtime.FollowUpItem) (turn.StartTurnCommand, error) {
	var envelope durableFollowUpPayload
	if err := json.Unmarshal(item.Payload, &envelope); err != nil {
		return turn.StartTurnCommand{}, err
	}
	if envelope.Command == nil {
		return turn.StartTurnCommand{}, errors.New("HTTP follow-up has no command")
	}
	cmd := *envelope.Command
	if cmd.BotID != key.BotID || cmd.ThreadID != key.SessionID || cmd.UserID == "" || cmd.TeamID == "" || cmd.DelegationResultID != "" || cmd.TaskResultReceipt != "" {
		return cmd, errors.New("HTTP follow-up authority does not match its queue")
	}
	cmd.NoDefer = true
	cmd.IdempotencyKey = "follow-up:" + string(item.ID)
	return cmd, nil
}

func provideDurableFollowUps(log *slog.Logger, lc fx.Lifecycle, runs ledger.Store, cfg config.Config, db *postgres.Store, botService *bots.Service, accounts *accounts.Service, sessions *thread.Service, routes *route.DBService) (*followupqueue.Backend, error) {
	if cfg.SessionRuntime.BackendOrDefault() != config.SessionRuntimeBackendMemory {
		return nil, nil
	}
	ttl, err := time.ParseDuration(cfg.SessionRuntime.StateTTLOrDefault())
	if err != nil {
		return nil, err
	}
	store, err := followupqueue.New(db.Pool(), cfg.Auth.AgentCredentialsEncryptionKey)
	if err != nil {
		log.Warn("durable queue at-rest encryption unavailable", slog.Any("error", err))
		store, err = followupqueue.New(db.Pool(), "")
		if err != nil {
			return nil, err
		}
	}
	backend := followupqueue.NewBackend(runtime.NewMemoryBackendWithTTL(ttl), store)
	permissions := &applicationBotPermissionChecker{bots: botService, accounts: accounts}
	contacts := route.NewThreadCoordinator(nil, routes, sessions)
	backend.AuthorizeHTTP = func(ctx context.Context, key runtime.Key, item runtime.FollowUpItem) error {
		cmd, err := httpFollowUpCommand(key, item)
		if err != nil {
			return err
		}
		if err = accounts.ValidateSession(ctx, cmd.UserID); err != nil {
			return err
		}
		sess, err := sessions.Get(ctx, key.SessionID)
		if err != nil {
			return err
		}
		if sess.BotID != key.BotID || sess.TeamID != cmd.TeamID {
			return errors.New("HTTP follow-up session scope changed")
		}
		manage, err := permissions.HasBotPermission(ctx, key.BotID, cmd.UserID, bots.PermissionManage)
		if err != nil {
			return err
		}
		if !manage {
			allowed, e := permissions.HasBotPermission(ctx, key.BotID, cmd.UserID, bots.PermissionChat)
			if e != nil {
				return e
			}
			if !allowed || sess.CreatedByUserID != cmd.UserID {
				return errors.New("HTTP follow-up access no longer granted")
			}
		}
		entry, err := contacts.InspectPrivateChat(ctx, key.BotID, cmd.UserID, "")
		if err != nil {
			return err
		}
		if entry.Session == nil || entry.Session.ID != key.SessionID {
			return errors.New("HTTP follow-up is no longer the original contact")
		}
		return nil
	}
	lc.Append(fx.Hook{OnStart: func(ctx context.Context) error {
		keys, err := store.Keys(ctx)
		if err != nil {
			log.WarnContext(ctx, "durable follow-up claim scan unavailable", slog.Any("error", err))
			return nil
		}
		for _, key := range keys {
			if err = reconcileFollowUpClaims(ctx, backend, key, runs); err != nil {
				log.WarnContext(ctx, "durable follow-up queue needs recovery", slog.String("session_id", key.SessionID), slog.Any("error", err))
			}
		}
		return nil
	}})
	return backend, nil
}

// Only the existing application and durable ledger may admit execution. This
// startup wake is the missing counterpart to the normal terminal observer;
// no additional scheduler, daemon, polling interval or retry budget is added.
func recoverDurableFollowUps(lc fx.Lifecycle, log *slog.Logger, backend *followupqueue.Backend, runs ledger.Store, app *application.Service, manager *runtime.Manager) {
	if backend == nil {
		return
	}
	lc.Append(fx.Hook{OnStart: func(ctx context.Context) error {
		keys, err := backend.Store.Keys(ctx)
		if err != nil {
			log.WarnContext(ctx, "durable follow-up startup scan unavailable", slog.Any("error", err))
			return nil
		}
		for _, key := range keys {
			if err = recoverDurableFollowUpKey(ctx, log, key, backend, runs, app, manager); err != nil {
				log.WarnContext(ctx, "durable follow-up remains unresolved", slog.String("session_id", key.SessionID), slog.Any("error", err))
			}
		}
		return nil
	}})
}

func recoverDurableFollowUpKey(ctx context.Context, log *slog.Logger, key runtime.Key, backend *followupqueue.Backend, runs ledger.Store, app *application.Service, manager *runtime.Manager) error {
	state, err := backend.Store.Read(ctx, key)
	if err != nil {
		return err
	}
	snapshot, err := manager.Snapshot(ctx, key.BotID, key.SessionID)
	if err != nil {
		return err
	}
	if snapshot.CurrentRunView != nil && runtime.IsActiveRunStatus(snapshot.CurrentRunView.Status) {
		return nil
	}
	var first *runtime.FollowUpItem
	for i := range state.Items {
		item := &state.Items[i]
		if item.Status == runtime.QueueAccepted && (first == nil || item.Position < first.Position) {
			first = item
		}
	}
	if first != nil && !state.HTTP[string(first.ID)] {
		return nil
	}
	triggers := map[string]bool{}
	for _, item := range state.Items {
		if !state.HTTP[string(item.ID)] {
			continue
		}
		if item.Status == runtime.QueueAccepted {
			triggers[item.EnqueuedDuringRunID] = true
		}
		if item.Status == runtime.QueueApplied {
			run, e := runs.GetByInvocation(ctx, key.SessionID, "follow-up:"+string(item.ID))
			if e != nil && !errors.Is(e, ledger.ErrRunNotFound) {
				return e
			}
			if e == nil && runIsTerminal(run) {
				triggers[run.RunID] = true
			}
		}
	}
	for trigger := range triggers {
		// A receipt alone never asserts that its source run has ended.
		source, e := runs.Get(ctx, trigger)
		if e != nil {
			return e
		}
		if source.SessionID != key.SessionID || !runIsTerminal(source) {
			continue
		}
		item, claim, claimed, e := backend.ClaimNextFollowUp(ctx, key, trigger)
		if e != nil {
			log.WarnContext(ctx, "durable HTTP follow-up remains pending", slog.Any("error", e))
			continue
		}
		if !claimed {
			continue
		}
		cmd, e := httpFollowUpCommand(key, item)
		if e != nil {
			return e
		}
		handle, e := app.StartTurn(context.WithoutCancel(ctx), cmd)
		if e != nil && !errors.Is(e, turn.ErrDuplicateTurn) {
			// Read admission before classifying a failed response. An admitted run
			// keeps its identity; an unreadable result keeps the claim unknown.
			run, lookup := runs.GetByInvocation(ctx, key.SessionID, cmd.IdempotencyKey)
			if lookup == nil && run.RunID != "" {
				if e = backend.ApplyFollowUp(ctx, key, claim); e != nil {
					return e
				}
				continue
			}
			if errors.Is(lookup, ledger.ErrRunNotFound) && errors.Is(e, turn.ErrSessionBusy) {
				if e = backend.ReleaseFollowUp(ctx, key, claim); e != nil {
					return e
				}
				break
			}
			if e = markFollowUpUnknown(ctx, backend, key, item.ID); e != nil {
				return e
			}
			continue
		}
		if e = backend.ApplyFollowUp(ctx, key, claim); e != nil {
			return e
		}
		if handle != nil {
			go drainRecoveredFollowUp(handle)
		}
		break
	}
	return nil
}

func runIsTerminal(run ledger.Run) bool {
	return run.State == ledger.StateCompleted || run.State == ledger.StateFailed || run.State == ledger.StateAborted || run.State == ledger.StateLost
}

func markFollowUpUnknown(ctx context.Context, b *followupqueue.Backend, key runtime.Key, id runtime.FollowUpItemID) error {
	return b.Store.Change(ctx, key, func(state *followupqueue.State) error {
		for i := range state.Items {
			item := &state.Items[i]
			if item.ID == id && item.Status == runtime.QueueClaimed {
				item.Status = "unknown"
				item.ErrorCode = "queue_admission_unconfirmed"
			}
		}
		return nil
	})
}

func reconcileFollowUpClaims(ctx context.Context, b *followupqueue.Backend, key runtime.Key, runs ledger.Store) error {
	before, err := b.Store.Read(ctx, key)
	if err != nil {
		return err
	}
	type resolution struct {
		original runtime.FollowUpItem
		admitted bool
	}
	resolved := map[runtime.FollowUpItemID]resolution{}
	unconfirmedPromotion := func(state *followupqueue.State, id runtime.FollowUpItemID) bool {
		promotion, ok := state.Promotions[string(id)]
		return ok && promotion.Steer.Status != runtime.QueueApplied && promotion.Steer.Status != runtime.QueueRejected && promotion.Steer.Status != runtime.QueueCanceled
	}
	// The ledger may share the queue's pool. Finish its reads before acquiring
	// the queue transaction; a single connection must suffice for recovery.
	for _, item := range before.Items {
		if item.Status != runtime.QueueClaimed || unconfirmedPromotion(&before, item.ID) {
			continue
		}
		run, lookupErr := runs.GetByInvocation(ctx, key.SessionID, "follow-up:"+string(item.ID))
		if lookupErr != nil && !errors.Is(lookupErr, ledger.ErrRunNotFound) {
			return lookupErr
		}
		if lookupErr == nil && run.RunID == "" {
			return errors.New("durable follow-up ledger returned an empty admission")
		}
		resolved[item.ID] = resolution{original: item, admitted: lookupErr == nil}
	}
	return b.Store.Change(ctx, key, func(state *followupqueue.State) error {
		// A promoted steer belongs to the dead live generation. Its saved
		// acceptance is not proof of consumption; preserve the original
		// destination but never return it as a fresh successful promotion.
		for i := range state.Items {
			item := &state.Items[i]
			if unconfirmedPromotion(state, item.ID) {
				item.Status = "unknown"
				item.ErrorCode = "queue_promotion_unconfirmed"
			}
		}
		for i := range state.Items {
			item := &state.Items[i]
			result, ok := resolved[item.ID]
			// A release/reclaim or another recovery may have changed the item
			// while the ledger was read. Never overwrite that newer receipt.
			if !ok || !reflect.DeepEqual(*item, result.original) {
				continue
			}
			if result.admitted {
				item.Status = runtime.QueueApplied
				continue
			}
			item.Status = "unknown"
			item.ErrorCode = "queue_admission_unconfirmed"
		}
		return nil
	})
}

func drainRecoveredFollowUp(handle turn.RunHandle) {
	events, errs := handle.Events(), handle.Errs()
	for events != nil || errs != nil {
		select {
		case _, ok := <-events:
			if !ok {
				events = nil
			}
		case _, ok := <-errs:
			if !ok {
				errs = nil
			}
		}
	}
}
