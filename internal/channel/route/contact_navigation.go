package route

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

var ErrContactConflict = errors.New("contact already bound or candidate unavailable")

// PrivateChatEntry is read-only. A tombstoned contact needs normal resolution;
// candidates are offered only before the account has a binding.
type PrivateChatEntry struct {
	Session    *thread.Thread  `json:"session,omitempty"`
	Candidates []thread.Thread `json:"candidates"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

func contactIDs(bot, user string) (pgtype.UUID, pgtype.UUID, error) {
	bid, e := uuid.Parse(bot)
	uid, ue := uuid.Parse(user)
	if e != nil || ue != nil || bid == uuid.Nil || uid == uuid.Nil {
		return pgtype.UUID{}, pgtype.UUID{}, ErrContactUnavailable
	}
	return pgtype.UUID{Bytes: bid, Valid: true}, pgtype.UUID{Bytes: uid, Valid: true}, nil
}

func (c *ThreadCoordinator) InspectPrivateChat(ctx context.Context, bot, user, after string, selected ...string) (PrivateChatEntry, error) {
	result := PrivateChatEntry{Candidates: []thread.Thread{}}
	bid, uid, err := contactIDs(bot, user)
	if err != nil {
		return result, err
	}
	routes, ok := c.routes.(*DBService)
	if !ok {
		return result, ErrContactUnavailable
	}
	q := routes.queries
	r, err := q.FindChatRoute(ctx, sqlc.FindChatRouteParams{BotID: bid, Platform: "web", ConversationID: "contact:" + uid.String()})
	if err == nil {
		if !r.ActiveSessionID.Valid || r.Platform != "web" || r.ConversationType.String != "private" || r.ChannelConfigID.Valid || r.ThreadID.String != "" {
			return result, ErrContactUnavailable
		}
		s, readErr := q.ReadContactSession(ctx, sqlc.ReadContactSessionParams{BotID: bid, SessionID: r.ActiveSessionID})
		if readErr != nil {
			return result, readErr
		}
		if s.CreatedByUserID != uid || s.RouteID != r.ID || (s.ChannelType.String != "local" && s.ChannelType.String != "") || s.Visibility != string(thread.VisibilityUser) || s.ParentSessionID.Valid {
			return result, ErrContactUnavailable
		}
		if s.DeletedAt.Valid {
			return result, nil
		}
		live, readErr := thread.NewService(c.logger, q, nil).Get(ctx, s.ID.String())
		if readErr != nil {
			return result, readErr
		}
		result.Session = &live
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	cursor := pgtype.UUID{}
	if after != "" {
		id, parseErr := uuid.Parse(after)
		if parseErr != nil {
			return result, ErrContactConflict
		}
		cursor = pgtype.UUID{Bytes: id, Valid: true}
	}
	ids, err := q.ListContactCandidates(ctx, sqlc.ListContactCandidatesParams{BotID: bid, UserID: uid, UseCursor: cursor.Valid, AfterID: cursor})
	if err != nil {
		return result, err
	}
	if len(ids) > 50 {
		ids = ids[:50]
		result.NextCursor = ids[49].ID.String()
	}
	for _, s := range ids {
		result.Candidates = append(result.Candidates, contactCandidateSummary(s))
	}
	if after == "" && len(selected) > 0 && selected[0] != "" {
		id, parseErr := uuid.Parse(selected[0])
		if parseErr != nil {
			return result, ErrContactConflict
		}
		candidate, readErr := q.GetContactCandidate(ctx, sqlc.GetContactCandidateParams{BotID: bid, UserID: uid, SessionID: pgtype.UUID{Bytes: id, Valid: true}})
		if readErr != nil && !errors.Is(readErr, pgx.ErrNoRows) {
			return result, readErr
		}
		if readErr == nil {
			found := false
			for _, s := range result.Candidates {
				if s.ID == candidate.ID.String() {
					found = true
					break
				}
			}
			if !found {
				result.Candidates = append([]thread.Thread{contactCandidateSummary(candidate)}, result.Candidates...)
			}
		}
	}
	return result, nil
}

// A candidate is the eligibility snapshot, not a second unfiltered read that
// could expose a session changed to internal in the meantime.
func contactCandidateSummary(s sqlc.BotSession) thread.Thread {
	return thread.Thread{ID: s.ID.String(), BotID: s.BotID.String(), CreatedByUserID: s.CreatedByUserID.String(), Title: s.Title, Type: s.Type, SessionMode: s.SessionMode, RuntimeType: s.RuntimeType, CreatedAt: s.CreatedAt.Time, UpdatedAt: s.UpdatedAt.Time}
}

// BindPrivateChat binds an eligible existing conversation once. The client
// uses the only verified candidate or an explicit choice between candidates.
// It never replaces another binding, merges history, or replays session_start.
func (c *ThreadCoordinator) BindPrivateChat(ctx context.Context, bot, user, session string) (thread.Thread, error) {
	bid, uid, err := contactIDs(bot, user)
	if err != nil {
		return thread.Thread{}, err
	}
	sid, err := uuid.Parse(session)
	if err != nil || sid == uuid.Nil {
		return thread.Thread{}, ErrContactConflict
	}
	pid := pgtype.UUID{Bytes: sid, Valid: true}
	routes, ok := c.routes.(*DBService)
	if !ok {
		return thread.Thread{}, ErrContactUnavailable
	}
	tx, ok := routes.queries.(contactTransactions)
	if !ok || !tx.SupportsTransactions() {
		return thread.Thread{}, ErrContactUnavailable
	}
	var result thread.Thread
	err = tx.InTx(ctx, func(q dbstore.Queries) error {
		if _, e := q.LockBotForSessionWrite(ctx, bid); e != nil {
			return e
		}
		stored, e := q.LockSessionForContact(ctx, sqlc.LockSessionForContactParams{BotID: bid, SessionID: pid})
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrContactConflict
		}
		if e != nil {
			return e
		}
		// Check identity before creating any route, including for administrators.
		if stored.CreatedByUserID != uid || stored.DeletedAt.Valid || stored.Visibility != string(thread.VisibilityUser) || stored.ParentSessionID.Valid || (stored.ChannelType.String != "local" && stored.ChannelType.String != "") {
			return ErrContactConflict
		}
		previous, lookupErr := q.FindChatRoute(ctx, sqlc.FindChatRouteParams{BotID: bid, Platform: "web", ConversationID: "contact:" + uid.String()})
		if lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows) {
			return lookupErr
		}
		created := errors.Is(lookupErr, pgx.ErrNoRows)
		if !created {
			if !previous.ActiveSessionID.Valid {
				return ErrContactUnavailable
			}
			if previous.ActiveSessionID != pid || stored.RouteID != previous.ID {
				return ErrContactConflict
			}
		} else {
			if stored.RouteID.Valid {
				return ErrContactConflict
			}
			_, e = q.InsertContactBindingRoute(ctx, sqlc.InsertContactBindingRouteParams{BotID: bid, ConversationID: "contact:" + uid.String(), ReplyTarget: pgtype.Text{String: bot, Valid: true}})
			if errors.Is(e, pgx.ErrNoRows) {
				return ErrContactConflict
			}
			if e != nil {
				return e
			}
		}
		r, e := q.EnsurePrivateChatRoute(ctx, sqlc.EnsurePrivateChatRouteParams{BotID: bid, ConversationID: "contact:" + uid.String(), ReplyTarget: pgtype.Text{String: bot, Valid: true}})
		if e != nil {
			return e
		}
		if r.Platform != "web" || r.ConversationType.String != "private" || r.ChannelConfigID.Valid || r.ThreadID.String != "" {
			return ErrContactUnavailable
		}
		if !created && (r.ID != previous.ID || r.ActiveSessionID != previous.ActiveSessionID) {
			return ErrContactConflict
		}
		if r.ActiveSessionID.Valid {
			// Idempotent readback after an uncertain response. Do not lock a different
			// session after Route: another device's confirmed choice always wins.
			if r.ActiveSessionID != pid || stored.RouteID != r.ID {
				return ErrContactConflict
			}
		} else {
			if !created || stored.RouteID.Valid {
				return ErrContactConflict
			}
			count, e := q.BindContactCandidate(ctx, sqlc.BindContactCandidateParams{BotID: bid, UserID: uid, SessionID: pid, RouteID: r.ID})
			if e != nil {
				return e
			}
			if count != 1 {
				return ErrContactConflict
			}
			if e = q.SetRouteActiveSession(ctx, sqlc.SetRouteActiveSessionParams{ID: r.ID, ActiveSessionID: pid}); e != nil {
				return e
			}
		}
		result, e = thread.NewService(c.logger, q, nil).Get(ctx, session)
		return e
	})
	return result, err
}
