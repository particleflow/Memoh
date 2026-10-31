package sessiontask

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	thread "github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/runtimekind"
)

const (
	activityCursorScope = "bot-activity"
	sessionRun          = "session_run"
)

// Activity contains only what the compact list needs. IDs are navigation
// references, never grants. No transcript, request, target or receipt is exposed.
type Activity struct {
	ID        string `json:"id" validate:"required"`
	Kind      string `json:"kind" validate:"required" enums:"bot,agent,task"`
	BotID     string `json:"bot_id" validate:"required"`
	SessionID string `json:"session_id" validate:"required"`
	Name      string `json:"name" validate:"required"`
	State     string `json:"state" validate:"required" enums:"accepted,submitted,running,waiting_decision,finishing"`
}

type ActivityPage struct {
	Items      []Activity `json:"items" validate:"required"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

func activeState(state string) bool {
	switch state {
	case "accepted", "submitted", "running", "waiting_decision", "finishing":
		return true
	}
	return false
}

// activityPath applies ordinary session-read policy to every real ancestor.
// Unlike the owned-task API, managers can see other readable Bot conversations;
// computer receipts still use the existing stricter owner/target projection.
func (s *Service) activityPath(ctx context.Context, bot, id string, readable func(thread.Thread) bool) ([]thread.Thread, error) {
	var path []thread.Thread
	seen := map[string]bool{}
	for id != "" {
		if seen[id] || len(path) >= maxDepth {
			return nil, ErrUnavailable
		}
		seen[id] = true
		t, err := s.sessions.Get(ctx, id)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) || errors.Is(err, pgx.ErrNoRows) || errors.Is(err, ErrUnavailable) {
				return nil, ErrUnavailable
			}
			return nil, err
		}
		if t.ID != id || t.BotID != bot || !readable(t) || (t.ParentThreadID != "" && t.Type != Subagent) {
			return nil, ErrUnavailable
		}
		path = append(path, t)
		id = t.ParentThreadID
	}
	if len(path) == 0 {
		return nil, ErrUnavailable
	}
	return path, nil
}

func (s *Service) activity(ctx context.Context, bot, user string, c dbstore.SessionTaskCandidate, readable func(thread.Thread) bool) (Activity, error) {
	p, err := s.activityPath(ctx, bot, c.SessionID, readable)
	if err != nil {
		return Activity{}, err
	}
	if _, err = s.scope(ctx, bot, user, p); err != nil {
		return Activity{}, err
	}
	a := Activity{ID: c.ID, BotID: bot, SessionID: c.SessionID, Name: p[0].Title, Kind: "bot"}
	if c.Name != "" {
		a.Name = c.Name
	}
	if c.Type == sessionRun {
		r, err := s.store.ReadSubagentRun(ctx, bot, c.SessionID)
		if errors.Is(err, db.ErrNotFound) {
			return Activity{}, ErrUnavailable
		}
		if err != nil {
			return Activity{}, err
		}
		if r.BotID != bot || r.SessionID != c.SessionID || r.RunID != c.ID || !activeState(r.State) {
			return Activity{}, ErrUnavailable
		}
		a.State = r.State
		if p[0].Type == Subagent || runtimekind.RequiresWorkspaceExec(p[0].RuntimeType) {
			a.Kind = "agent"
		}
		return a, nil
	}
	if c.Type != ComputerOperation {
		return Activity{}, ErrUnavailable
	}
	t, err := s.project(ctx, bot, user, p[len(p)-1].ID, c)
	if err != nil {
		return Activity{}, err
	}
	if !activeState(t.Status) {
		return Activity{}, ErrUnavailable
	}
	a.Kind, a.State = "task", t.Status
	return a, nil
}

// ListActivity is a read of existing ledgers, independent of the open Bot/tab.
// One bounded scan per page; an empty filtered page can still continue.
func (s *Service) ListActivity(ctx context.Context, bot, user string, limit int, raw string, readable func(thread.Thread) bool) (ActivityPage, error) {
	out := ActivityPage{Items: []Activity{}}
	if limit < 1 || limit > 100 {
		return out, ErrCursor
	}
	reader, ok := s.store.(dbstore.SessionActivityReader)
	if !ok {
		return out, ErrUnavailable
	}
	c, err := s.decodeCursor(raw, bot, user, activityCursorScope)
	if err != nil {
		return out, err
	}
	rows, err := reader.ListSessionActivityCandidates(ctx, dbstore.SessionTaskFilter{BotID: bot, UserID: user, SnapshotAt: c.Snapshot, Before: c.Before, Limit: scanBudget + 1})
	if err != nil {
		return out, err
	}
	for i, row := range rows {
		if i == scanBudget || len(out.Items) == limit {
			out.NextCursor, err = s.encodeCursor(c)
			return out, err
		}
		c.Before = row
		item, err := s.activity(ctx, bot, user, row, readable)
		if errors.Is(err, ErrUnavailable) {
			continue
		}
		if err != nil {
			return ActivityPage{}, err
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}
