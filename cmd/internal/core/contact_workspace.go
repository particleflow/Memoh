package core

import (
	"context"
	"strings"

	"github.com/felinics/memoh/internal/agent/application"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/workspace"
)

// Fresh queue/result contexts do not carry the original request's workspace
// selection. Restore an owned Web contact's persisted selection before
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
	// The native text follow-up queue records sender/session identity but no
	// channel. Only an owned Web route may opt into this contact adapter.
	if req.CurrentChannel == "" && s.ChannelType != "" && s.ChannelType != "local" && s.ChannelType != "web" {
		return ctx, req, nil
	}
	// Reuse current owner/team/chat authorization and strict stored-selection
	// validation. An invalid, revoked or inaccessible target is never replaced.
	binding, err := h.service.Authority.ContactSource(ctx, req.UserID, req.BotID, req.ThreadID)
	if err != nil {
		return ctx, req, err
	}
	if binding.SourceSelection == "" {
		return ctx, req, nil
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
	if binding.SourceSelection != workspace.WorkspaceTargetNative {
		allowed, err := h.service.Authority.Permissions.Allowed(ctx, req.UserID, req.BotID, bots.PermissionWorkspaceRead)
		if err != nil {
			return ctx, req, err
		}
		if !allowed {
			return ctx, req, botdelegation.ErrDenied
		}
		// Preserve explicit target resolution and its current registration,
		// capability, ownership and availability errors. Never select Primary.
		req.WorkspaceTargetID = binding.SourceSelection
	}
	ctx = workspace.WithWorkspaceTarget(ctx, binding.SourceSelection)
	return ctx, req, nil
}
