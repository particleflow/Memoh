package imagejob

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

type Accounts interface {
	ValidateSession(context.Context, string) error
	IsAdmin(context.Context, string) (bool, error)
}
type BotAccess interface {
	ResolveUserPermissions(context.Context, string, string, bool) ([]string, error)
}
type Sessions interface {
	Get(context.Context, string) (thread.Thread, error)
}
type Authority struct {
	Queries  dbstore.Queries
	Accounts Accounts
	Bots     BotAccess
	Sessions Sessions
}

// Origin is host-owned billing identity, not a grant or a model-selected task.
// Children inherit the original root run unchanged. The DB run's turn ID is the
// stable business identity even when a new child is created for the same request.
type (
	Origin    struct{ BotID, UserID, SessionID, RunID string }
	originKey struct{}
)

func WithOrigin(ctx context.Context, o Origin) context.Context {
	if o.BotID == "" || o.UserID == "" || o.SessionID == "" || o.RunID == "" {
		return ctx
	}
	if _, ok := ctx.Value(originKey{}).(Origin); ok {
		return ctx
	}
	return context.WithValue(ctx, originKey{}, o)
}

func (a *Authority) Bind(ctx context.Context, bot, user, session string) (Binding, string, error) {
	b := Binding{BotID: bot, UserID: user, SessionID: session}
	path, err := a.path(ctx, b)
	if err != nil {
		return Binding{}, "", ErrDenied
	}
	root := path[len(path)-1]
	b.TeamID, b.RootSessionID = root.TeamID, root.ID
	o, ok := ctx.Value(originKey{}).(Origin)
	if !ok || o.BotID != bot || o.UserID != user || o.SessionID != root.ID {
		return Binding{}, "", ErrDenied
	}
	id, err := db.ParseUUID(o.RunID)
	if err != nil {
		return Binding{}, "", ErrDenied
	}
	run, err := a.Queries.GetSessionRun(ctx, id)
	if err != nil || uuidString(run.BotID) != bot || uuidString(run.SessionID) != root.ID || uuidString(run.TeamID) != root.TeamID || !run.TurnID.Valid {
		return Binding{}, "", ErrDenied
	}
	if err := a.Authorize(ctx, b, nil); err != nil {
		return Binding{}, "", err
	}
	return b, uuidString(run.TurnID), nil
}

func (a *Authority) path(ctx context.Context, b Binding) ([]thread.Thread, error) {
	if a == nil || a.Queries == nil || a.Sessions == nil || a.Accounts == nil || a.Bots == nil || b.UserID == "" {
		return nil, ErrDenied
	}
	var path []thread.Thread
	seen := map[string]bool{}
	for id := b.SessionID; id != ""; {
		if seen[id] || len(path) >= 64 {
			return nil, ErrDenied
		}
		seen[id] = true
		s, err := a.Sessions.Get(ctx, id)
		if err != nil || s.ID != id || s.BotID != b.BotID || s.CreatedByUserID != b.UserID || s.TeamID == "" || (s.Type != thread.TypeChat && s.Type != thread.TypeSubagent) || (s.ParentThreadID != "" && s.Type != thread.TypeSubagent) {
			return nil, ErrDenied
		}
		if len(path) > 0 && s.TeamID != path[0].TeamID {
			return nil, ErrDenied
		}
		path = append(path, s)
		id = s.ParentThreadID
	}
	if len(path) == 0 || path[len(path)-1].Type != thread.TypeChat {
		return nil, ErrDenied
	}
	return path, nil
}

func (a *Authority) Authorize(ctx context.Context, b Binding, refs []string) error {
	path, err := a.path(ctx, b)
	if err != nil {
		return ErrDenied
	}
	root := path[len(path)-1]
	if root.TeamID != b.TeamID || root.ID != b.RootSessionID {
		return ErrDenied
	}
	if a.Accounts.ValidateSession(ctx, b.UserID) != nil {
		return ErrDenied
	}
	admin, err := a.Accounts.IsAdmin(ctx, b.UserID)
	if err != nil {
		return ErrDenied
	}
	permissions, err := a.Bots.ResolveUserPermissions(ctx, b.BotID, b.UserID, admin)
	if err != nil || !bots.HasPermission(permissions, bots.PermissionChat) {
		return ErrDenied
	}
	botID, err := db.ParseUUID(b.BotID)
	if err != nil {
		return ErrDenied
	}
	for _, ref := range refs {
		found := false
		for _, s := range path {
			sessionID, err := db.ParseUUID(s.ID)
			if err != nil {
				return ErrDenied
			}
			yes, err := a.Queries.SessionHasMediaAsset(ctx, sqlc.SessionHasMediaAssetParams{BotID: botID, SessionID: sessionID, ContentHash: ref})
			if err != nil {
				return ErrDenied
			}
			if yes {
				found = true
				break
			}
		}
		if !found {
			return ErrDenied
		}
	}
	return nil
}

// Results records only IDs emitted by the real image service during this child
// invocation. The outer spawn adapter re-reads them with the parent's identity.
// Model text and model-supplied artifact hashes never enter this collector.
type Results struct {
	mu  sync.Mutex
	ids map[string]bool
}
type resultsKey struct{}

func WithResults(ctx context.Context) (context.Context, *Results) {
	r := &Results{ids: map[string]bool{}}
	return context.WithValue(ctx, resultsKey{}, r), r
}

func (r *Results) IDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.ids))
	for id := range r.ids {
		out = append(out, id)
	}
	return out
}

func track(ctx context.Context, result Result) {
	if result.State != "ready" {
		return
	}
	if r, ok := ctx.Value(resultsKey{}).(*Results); ok {
		r.mu.Lock()
		r.ids[result.TaskID] = true
		r.mu.Unlock()
	}
}

// BusinessKey is deterministic within a host-admitted root turn. New child IDs
// and new tool-call IDs cannot create a fresh charge for the same image request.
func BusinessKey(turn string, r Request) string {
	r.Key = ""
	return digest(struct {
		Turn    string
		Request Request
	}{turn, r})
}

func uuidString(v pgtype.UUID) string {
	if !v.Valid {
		return ""
	}
	return uuid.UUID(v.Bytes).String()
}
