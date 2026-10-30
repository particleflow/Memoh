package core

import (
	"context"
	"strings"

	"github.com/felinics/memoh/internal/agent/application"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/workspace"
)

// Fresh queue/result contexts do not carry the original request's workspace
// selection. Restore an owned native Web contact's persisted selection before
// the ordinary resolver can fall through to the Bot's unrelated primary mount.
// RuntimeType only limits this adapter's scope; it never chooses a workspace.
func (h *delegationHost) prepareContactWorkspace(ctx context.Context, req application.ChatRequest) (context.Context, application.ChatRequest, error) {
	if (req.CurrentChannel != "web" && req.CurrentChannel != "") || strings.TrimSpace(req.WorkspaceTargetID) != "" {
		return ctx, req, nil
	}
	s, err := h.service.Authority.Sessions.Get(ctx, req.ThreadID)
	if err != nil {
		return ctx, req, err
	}
	if s.Type != thread.TypeChat || s.ParentThreadID != "" || s.WorkdirID != "" || (s.RuntimeType != "" && s.RuntimeType != thread.RuntimeModel) {
		return ctx, req, nil
	}
	id, _ := s.Metadata["workspace_target_id"].(string)
	snapshot, _ := s.Metadata["workspace_target"].(map[string]any)
	snapshotID, _ := snapshot["target_id"].(string)
	// Limit this repair to recorded native placement. Remote Web contacts may
	// use ordinary bridges outside the delegation device-profile contract.
	// Neither a runtime type nor a missing/remote selection opts them in.
	if strings.TrimSpace(id) != workspace.WorkspaceTargetNative && strings.TrimSpace(snapshotID) != workspace.WorkspaceTargetNative {
		return ctx, req, nil
	}
	// The native text follow-up queue records sender/session identity but no
	// channel. Only an owned Web route may opt into this contact adapter.
	if req.CurrentChannel == "" && s.ChannelType != "" && s.ChannelType != "local" && s.ChannelType != "web" {
		return ctx, req, nil
	}
	// Reuse current owner/team/chat authorization and strict stored-selection
	// validation. An invalid, revoked or inaccessible target is never replaced.
	binding, err := h.service.Authority.Source(ctx, req.UserID, req.BotID, req.ThreadID)
	if err != nil {
		return ctx, req, err
	}
	if req.CurrentChannel == "" {
		routing, err := delegationSourceRoute(ctx, binding, h.service.Authority.Sessions, h.routes)
		if err != nil {
			return ctx, req, err
		}
		if routing.Channel != "web" {
			return ctx, req, nil
		}
	}
	ctx = workspace.WithWorkspaceTarget(ctx, binding.SourceSelection)
	return ctx, req, nil
}
