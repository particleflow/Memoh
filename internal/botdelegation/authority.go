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
	SourceSelection                                            string
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
	OwnedNativeHerdrRuntime(context.Context, string, string) (dbstore.UserRuntimeRecord, error)
	OwnedHTTPMCPConnectionBot(context.Context, string, string) (string, error)
}

type AssetLinks interface {
	SessionHasAsset(context.Context, string, string, string) (bool, error)
	ResultHasAsset(context.Context, string, string, string, string) (bool, error)
}

type Authority struct {
	Permissions Permissions
	Sessions    Sessions
	Workdirs    Workdirs
	Mounts      Mounts
	Assets      AssetLinks
}

// Source authorizes an owned source conversation before listing its handoffs.
// Each returned record still requires both Bots' current permissions.
func (a *Authority) Source(ctx context.Context, user, bot, session string) (Identity, error) {
	b := Identity{UserID: user, SourceBotID: bot, SourceSessionID: session}
	path, err := a.sourcePath(ctx, b)
	if err != nil || a.Permissions == nil || a.Permissions.ValidateSession(ctx, user) != nil {
		return Identity{}, ErrDenied
	}
	b.TeamID, b.SourceRootID = path[0].TeamID, path[len(path)-1].ID
	team, err := a.Permissions.BotTeam(ctx, bot)
	if err != nil || team != b.TeamID {
		return Identity{}, ErrDenied
	}
	ok, err := a.Permissions.Allowed(ctx, user, bot, bots.PermissionChat)
	if err != nil || !ok {
		return Identity{}, ErrDenied
	}
	b.SourceSelection, err = a.selection(ctx, b, path)
	return b, err
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
	b.SourceSelection = selected
	return b, selected, nil
}

// Authorize rechecks stored identity against current data. An administrator's
// Bot access does not waive source-session ownership or same-team membership.
func (a *Authority) Authorize(ctx context.Context, b Identity) (string, error) {
	path, err := a.authorizeHistory(ctx, b)
	if err != nil {
		return "", err
	}
	selected, err := a.selection(ctx, b, path)
	if err != nil || (b.SourceSelection != "" && b.SourceSelection != selected) {
		return "", ErrDenied
	}
	return selected, nil
}

// authorizeHistory checks conversation ownership and live Bot grants without
// transferring an old execution binding to the currently selected computer.
func (a *Authority) authorizeHistory(ctx context.Context, b Identity) ([]thread.Thread, error) {
	path, err := a.sourcePath(ctx, b)
	if err != nil || b.TeamID == "" || path[0].TeamID != b.TeamID || path[len(path)-1].ID != b.SourceRootID {
		return nil, ErrDenied
	}
	if err := a.authorizeBots(ctx, b); err != nil {
		return nil, err
	}
	if b.TargetSessionID != "" {
		s, err := a.Sessions.Get(ctx, b.TargetSessionID)
		if err != nil || s.ID != b.TargetSessionID || s.TeamID != b.TeamID || s.BotID != b.TargetBotID || s.CreatedByUserID != b.UserID || s.ParentThreadID != "" || s.Type != thread.TypeChat {
			return nil, ErrDenied
		}
	}
	return path, nil
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
		if _, _, err := a.registration(ctx, b.SourceBotID, selected); err != nil {
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
	if err != nil || a.Mounts == nil || pair.SourceMountID == "" || pair.TargetMountID == "" || pair.RuntimeID == "" || pair.Fingerprint == "" || (selected != "native" && selected != pair.SourceMountID) {
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
		m, t, err := a.registration(ctx, side.bot, side.mount)
		if err != nil || m.RuntimeID != pair.RuntimeID || agenvo.TargetFingerprint(t) != pair.Fingerprint {
			return ErrDenied
		}
	}
	return nil
}

// registration is a database-only boundary check. ResolveTarget is deliberately
// not used: that execution resolver requires workspace_exec, which must not be
// implied by chat, original-media reads, or read-only device authorization.
func (a *Authority) registration(ctx context.Context, bot, mount string) (dbstore.BotRemoteRuntimeBindingRecord, agenvo.Target, error) {
	if a.Mounts == nil {
		return dbstore.BotRemoteRuntimeBindingRecord{}, agenvo.Target{}, ErrDenied
	}
	m, err := a.Mounts.GetMount(ctx, bot, mount)
	if err != nil || m.BotID != bot || m.ID != mount || m.RuntimeID == "" || m.RuntimeRevoked || m.RuntimeUserID == "" || m.RuntimeUserID != m.BotOwnerUserID {
		return dbstore.BotRemoteRuntimeBindingRecord{}, agenvo.Target{}, ErrDenied
	}
	t, err := agenvo.DecodeRuntimeTarget(m.RuntimeProvider, m.RuntimeID, m.RuntimeProviderConfig)
	if err != nil {
		return dbstore.BotRemoteRuntimeBindingRecord{}, agenvo.Target{}, ErrDenied
	}
	if t.NativeRuntimeID != "" {
		// The mount alone does not establish activation. Reuse the existing
		// owner-scoped registry query and reject a changed registration snapshot.
		r, err := a.Mounts.OwnedNativeHerdrRuntime(ctx, m.RuntimeUserID, m.RuntimeID)
		if err != nil || r.ID != m.RuntimeID || r.UserID != m.RuntimeUserID || r.Provider != m.RuntimeProvider || r.ActivatedAt.IsZero() {
			return dbstore.BotRemoteRuntimeBindingRecord{}, agenvo.Target{}, ErrDenied
		}
		current, err := agenvo.DecodeRuntimeTarget(r.Provider, r.ID, r.ProviderConfig)
		if err != nil || agenvo.TargetFingerprint(current) != agenvo.TargetFingerprint(t) {
			return dbstore.BotRemoteRuntimeBindingRecord{}, agenvo.Target{}, ErrDenied
		}
	} else {
		// Retain authorization of historical registrations for receipt reads;
		// new plans exclude them and production execution keeps Agenvo retired.
		if _, err = a.Mounts.OwnedHTTPMCPConnectionBot(ctx, m.RuntimeUserID, t.ConnectionID); err != nil {
			return dbstore.BotRemoteRuntimeBindingRecord{}, agenvo.Target{}, ErrDenied
		}
	}
	return m, t, nil
}

// PlanDevice pins two current mounts of the same activated native Runtime.
// A plan grants no workspace permission; every tool still rechecks both Bots.
func (a *Authority) PlanDevice(ctx context.Context, b Identity, source, target string) (DevicePair, error) {
	selected, err := a.Authorize(ctx, b)
	if err != nil || (selected != "native" && selected != source) {
		return DevicePair{}, ErrDenied
	}
	m, t, err := a.registration(ctx, b.SourceBotID, source)
	if err != nil || m.RuntimeProvider != "memoh" {
		return DevicePair{}, ErrDenied
	}
	tm, tt, err := a.registration(ctx, b.TargetBotID, target)
	if err != nil || tm.RuntimeProvider != "memoh" || tm.RuntimeID != m.RuntimeID || agenvo.TargetFingerprint(t) != agenvo.TargetFingerprint(tt) {
		return DevicePair{}, ErrDenied
	}
	return DevicePair{SourceMountID: m.ID, TargetMountID: tm.ID, RuntimeID: m.RuntimeID, Fingerprint: agenvo.TargetFingerprint(t)}, nil
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
		ok, err := a.Assets.ResultHasAsset(ctx, b.TargetBotID, b.UserID, b.TargetSessionID, hash)
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
