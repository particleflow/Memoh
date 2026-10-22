// Package botdelegation coordinates authorized handoffs between existing Bots.
// A handoff is not a grant: both Bots and every source ancestor remain subject
// to the initiating user's current permissions.
package botdelegation

import (
	"context"
	"errors"
	"strings"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/workdir"
)

// ErrDenied deliberately does not identify a Bot, computer, or attachment.
var ErrDenied = errors.New("delegation is unavailable in this conversation")

type Identity struct {
	TeamID, UserID, SourceBotID, SourceSessionID, SourceRootID string
	TargetBotID, TargetSessionID                               string
}

// DevicePair names the two existing mounts of one runtime. Mount IDs are
// Bot-specific, so comparing mount IDs across Bots would reject valid sharing.
// These values are private ledger facts, never model-supplied authority.
type DevicePair struct {
	SourceMountID, TargetMountID, RuntimeID string
	Fingerprint                             string
}

type Permissions interface {
	ValidateSession(context.Context, string) error
	BotTeam(context.Context, string) (string, error)
	Allowed(context.Context, string, string, string) (bool, error)
}

type Sessions interface {
	Get(context.Context, string) (thread.Thread, error)
}

type Workdirs interface {
	Get(context.Context, string, string) (workdir.Workdir, error)
}

type Mounts interface {
	GetMount(context.Context, string, string) (dbstore.BotRemoteRuntimeBindingRecord, error)
	GetPrimaryMount(context.Context, string) (dbstore.BotRemoteRuntimeBindingRecord, error)
}

type Targets interface {
	ResolveTarget(context.Context, agenvo.Scope) (agenvo.Target, error)
}

type AssetLinks interface {
	SessionHasAsset(context.Context, string, string, string) (bool, error)
}

type Authority struct {
	Permissions Permissions
	Sessions    Sessions
	Workdirs    Workdirs
	Mounts      Mounts
	Targets     Targets
	Assets      AssetLinks
}

// Bind checks the caller's complete owned source ancestry and both Bot grants.
// The target session is deliberately separate; it is never attached to another
// Bot's parent_session_id tree or re-labelled as a same-Bot subagent.
func (a *Authority) Bind(ctx context.Context, user, sourceBot, sourceSession, targetBot string) (Identity, string, error) {
	b := Identity{UserID: user, SourceBotID: sourceBot, SourceSessionID: sourceSession, TargetBotID: targetBot}
	path, err := a.sourcePath(ctx, b)
	if err != nil {
		return Identity{}, "", ErrDenied
	}
	b.TeamID, b.SourceRootID = path[0].TeamID, path[len(path)-1].ID
	if err := a.authorizeBots(ctx, b); err != nil {
		return Identity{}, "", err
	}
	selected, err := a.selection(ctx, b, path)
	if err != nil {
		return Identity{}, "", err
	}
	return b, selected, nil
}

// Authorize rechecks stored identity against current data. An administrator's
// Bot access does not waive source-session ownership or same-team membership.
func (a *Authority) Authorize(ctx context.Context, b Identity) (string, error) {
	path, err := a.sourcePath(ctx, b)
	if err != nil || b.TeamID == "" || path[0].TeamID != b.TeamID || path[len(path)-1].ID != b.SourceRootID {
		return "", ErrDenied
	}
	if err := a.authorizeBots(ctx, b); err != nil {
		return "", err
	}
	if b.TargetSessionID != "" {
		s, err := a.Sessions.Get(ctx, b.TargetSessionID)
		if err != nil || s.ID != b.TargetSessionID || s.TeamID != b.TeamID || s.BotID != b.TargetBotID || s.CreatedByUserID != b.UserID || s.ParentThreadID != "" || s.Type != thread.TypeChat {
			return "", ErrDenied
		}
	}
	return a.selection(ctx, b, path)
}

func (a *Authority) authorizeBots(ctx context.Context, b Identity) error {
	if a.Permissions == nil || b.UserID == "" || b.SourceBotID == "" || b.TargetBotID == "" || b.SourceBotID == b.TargetBotID || a.Permissions.ValidateSession(ctx, b.UserID) != nil {
		return ErrDenied
	}
	for _, bot := range []string{b.SourceBotID, b.TargetBotID} {
		team, err := a.Permissions.BotTeam(ctx, bot)
		if err != nil || team != b.TeamID {
			return ErrDenied
		}
		allowed, err := a.Permissions.Allowed(ctx, b.UserID, bot, bots.PermissionChat)
		if err != nil || !allowed {
			return ErrDenied
		}
	}
	return nil
}

func (a *Authority) sourcePath(ctx context.Context, b Identity) ([]thread.Thread, error) {
	if a == nil || a.Sessions == nil || b.UserID == "" || b.SourceBotID == "" || b.SourceSessionID == "" {
		return nil, ErrDenied
	}
	var path []thread.Thread
	seen := map[string]bool{}
	for id := b.SourceSessionID; id != ""; {
		if seen[id] || len(path) >= 64 {
			return nil, ErrDenied
		}
		seen[id] = true
		s, err := a.Sessions.Get(ctx, id)
		if err != nil || s.ID != id || s.BotID != b.SourceBotID || s.CreatedByUserID != b.UserID || s.TeamID == "" || (s.Type != thread.TypeChat && s.Type != thread.TypeSubagent) || (s.ParentThreadID != "" && s.Type != thread.TypeSubagent) {
			return nil, ErrDenied
		}
		if len(path) > 0 && path[0].TeamID != s.TeamID {
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

func (a *Authority) selection(ctx context.Context, b Identity, path []thread.Thread) (string, error) {
	selected := "native"
	for i := len(path) - 1; i >= 0; i-- {
		s := path[i]
		candidate := ""
		if s.WorkdirID != "" {
			if a.Workdirs == nil {
				return "", ErrDenied
			}
			w, err := a.Workdirs.Get(ctx, b.SourceBotID, s.WorkdirID)
			if err != nil || w.ID != s.WorkdirID || w.BotID != b.SourceBotID || w.WorkspaceTargetID == "" {
				return "", ErrDenied
			}
			candidate = w.WorkspaceTargetID
		} else {
			var err error
			candidate, err = storedSelection(s.Metadata)
			if err != nil {
				return "", err
			}
			if candidate == "" && i == len(path)-1 {
				if a.Mounts == nil {
					return "", ErrDenied
				}
				m, err := a.Mounts.GetPrimaryMount(ctx, b.SourceBotID)
				switch {
				case errors.Is(err, db.ErrNotFound):
					candidate = "native"
				case err != nil || m.BotID != b.SourceBotID || m.ID == "":
					return "", ErrDenied
				default:
					candidate = m.ID
				}
			}
		}
		if candidate == "" {
			continue
		}
		if selected != "native" && selected != candidate {
			return "", ErrDenied
		}
		selected = candidate
	}
	if selected != "native" {
		if a.Targets == nil {
			return "", ErrDenied
		}
		if _, err := a.Targets.ResolveTarget(ctx, agenvo.Scope{BotID: b.SourceBotID, UserID: b.UserID, TargetID: selected}); err != nil {
			return "", ErrDenied
		}
	}
	return selected, nil
}

func storedSelection(metadata map[string]any) (string, error) {
	id := ""
	if raw, ok := metadata["workspace_target_id"]; ok {
		v, valid := raw.(string)
		if !valid || strings.TrimSpace(v) == "" {
			return "", ErrDenied
		}
		id = strings.TrimSpace(v)
	}
	if raw, ok := metadata["workspace_target"]; ok {
		m, valid := raw.(map[string]any)
		if !valid {
			return "", ErrDenied
		}
		v, valid := m["target_id"].(string)
		v = strings.TrimSpace(v)
		if !valid || v == "" || (id != "" && id != v) {
			return "", ErrDenied
		}
		id = v
	}
	return id, nil
}

// AuthorizeDevice checks the intersection, including live registration and
// permissions. It never contacts a device. A PC selection is compared with its
// source mount; the target Bot can hold a different mount of the same runtime.
func (a *Authority) AuthorizeDevice(ctx context.Context, b Identity, pair DevicePair, permission string) error {
	selected, err := a.Authorize(ctx, b)
	if err != nil || a.Mounts == nil || a.Targets == nil || pair.SourceMountID == "" || pair.TargetMountID == "" || pair.RuntimeID == "" || pair.Fingerprint == "" || (selected != "native" && selected != pair.SourceMountID) {
		return ErrDenied
	}
	switch permission {
	case bots.PermissionWorkspaceRead, bots.PermissionWorkspaceWrite, bots.PermissionWorkspaceExec:
	default:
		return ErrDenied
	}
	for _, side := range []struct{ bot, mount string }{{b.SourceBotID, pair.SourceMountID}, {b.TargetBotID, pair.TargetMountID}} {
		allowed, err := a.Permissions.Allowed(ctx, b.UserID, side.bot, permission)
		if err != nil || !allowed {
			return ErrDenied
		}
		m, err := a.Mounts.GetMount(ctx, side.bot, side.mount)
		if err != nil || m.BotID != side.bot || m.ID != side.mount || m.RuntimeID != pair.RuntimeID || m.RuntimeRevoked || m.RuntimeUserID == "" || m.RuntimeUserID != m.BotOwnerUserID {
			return ErrDenied
		}
		t, err := a.Targets.ResolveTarget(ctx, agenvo.Scope{BotID: side.bot, UserID: b.UserID, TargetID: side.mount})
		if err != nil || agenvo.TargetFingerprint(t) != pair.Fingerprint {
			return ErrDenied
		}
	}
	return nil
}

// AuthorizeAsset accepts a content hash only when the original is linked to
// this owned source ancestry (input) or the exact specialist session (result).
// No broad Bot+hash fallback, URLs, paths, or copied bytes establish a link.
func (a *Authority) AuthorizeAsset(ctx context.Context, b Identity, hash string, result bool) error {
	if _, err := a.Authorize(ctx, b); err != nil || a.Assets == nil || len(hash) != 64 {
		return ErrDenied
	}
	for _, c := range hash {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return ErrDenied
		}
	}
	if result {
		if b.TargetSessionID == "" {
			return ErrDenied
		}
		ok, err := a.Assets.SessionHasAsset(ctx, b.TargetBotID, b.TargetSessionID, hash)
		if err != nil || !ok {
			return ErrDenied
		}
		return nil
	}
	path, err := a.sourcePath(ctx, b)
	if err != nil {
		return ErrDenied
	}
	for _, s := range path {
		ok, err := a.Assets.SessionHasAsset(ctx, b.SourceBotID, s.ID, hash)
		if err != nil {
			return ErrDenied
		}
		if ok {
			return nil
		}
	}
	return ErrDenied
}
