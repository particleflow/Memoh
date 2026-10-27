package botdelegation

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/toolcontext"
)

// ForSession includes same-Bot subagents of a delegated specialist. Persisted
// ancestry and ownership, not caller metadata, establish the inherited scope.
func (s *Service) ForSession(ctx context.Context, bot, user, session string) (Record, error) {
	seen := map[string]bool{}
	var path []thread.Thread
	for id := session; id != ""; {
		if seen[id] || len(seen) >= 64 {
			return Record{}, ErrDenied
		}
		seen[id] = true
		t, err := s.Authority.Sessions.Get(ctx, id)
		if err != nil || t.ID != id {
			return Record{}, ErrDenied
		}
		path = append(path, t)
		r, err := s.Ledger.ForTarget(ctx, bot, id)
		if err == nil {
			for _, ancestor := range path {
				if r.Identity.UserID != user || r.Identity.TeamID != ancestor.TeamID || ancestor.BotID != bot || ancestor.CreatedByUserID != user {
					return Record{}, ErrDenied
				}
			}
			if _, err = s.Authority.Authorize(ctx, r.Identity); err != nil {
				return Record{}, err
			}
			return r, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return Record{}, err
		}
		if t.ParentThreadID != "" && t.Type != thread.TypeSubagent {
			return Record{}, ErrDenied
		}
		id = t.ParentThreadID
	}
	return Record{}, pgx.ErrNoRows
}

// BindScope is reconstructed on later turns. It never clears ComputerScope or
// replaces a source binding; the destination's fresh host turn uses its own
// authorized mount of the recorded runtime before tools are assembled.
func (s *Service) BindScope(ctx context.Context, r Record) context.Context {
	check := func(ctx context.Context) error { _, err := s.Authority.Authorize(ctx, r.Identity); return err }
	return toolcontext.WithDelegationScope(ctx, &toolcontext.DelegationScope{
		Check: check,
		Target: func(ctx context.Context, bot, target, permission string) error {
			if err := check(ctx); err != nil {
				return err
			}
			if bot != r.Identity.TargetBotID {
				return ErrDenied
			}
			if target == "" {
				target = r.Payload.TargetSelection
			}
			if target == "native" {
				if r.SourceSelection != "native" && r.Payload.TargetSelection != "native" {
					return ErrDenied
				}
				if permission != "" {
					for _, b := range []string{r.Identity.SourceBotID, r.Identity.TargetBotID} {
						ok, err := s.Authority.Permissions.Allowed(ctx, r.Identity.UserID, b, permission)
						if err != nil || !ok {
							return ErrDenied
						}
					}
				}
				return nil
			}
			for _, p := range r.Payload.Devices {
				if p.TargetMountID != target {
					continue
				}
				if permission != "" {
					return s.Authority.AuthorizeDevice(ctx, r.Identity, p, permission)
				}
				for _, side := range []struct{ bot, mount string }{{r.Identity.SourceBotID, p.SourceMountID}, {r.Identity.TargetBotID, p.TargetMountID}} {
					m, t, err := s.Authority.registration(ctx, side.bot, side.mount)
					if err != nil || m.RuntimeID != p.RuntimeID || agenvo.TargetFingerprint(t) != p.Fingerprint {
						return ErrDenied
					}
				}
				return nil
			}
			return ErrDenied
		},
		Targets: func(ctx context.Context, bot string) ([]string, error) {
			if err := check(ctx); err != nil || bot != r.Identity.TargetBotID {
				return nil, ErrDenied
			}
			ids := []string{}
			if r.SourceSelection == "native" || r.Payload.TargetSelection == "native" {
				ids = append(ids, "native")
			}
			for _, p := range r.Payload.Devices {
				if s.Authority.AuthorizeDevice(ctx, r.Identity, p, bots.PermissionWorkspaceRead) == nil {
					ids = append(ids, p.TargetMountID)
				}
			}
			return ids, nil
		},
	})
}
