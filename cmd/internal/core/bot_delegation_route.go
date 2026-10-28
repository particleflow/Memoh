package core

import (
	"context"

	"github.com/felinics/memoh/internal/agent/application"
	"github.com/felinics/memoh/internal/agent/turn"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/channel/route"
	"github.com/felinics/memoh/internal/chat/thread"
)

type delegationRouteReader interface {
	GetByID(context.Context, string) (route.Route, error)
}

func (h *delegationHost) SourceRoute(ctx context.Context, b botdelegation.Identity) (botdelegation.ReturnRoute, error) {
	return delegationSourceRoute(ctx, b, h.sessions, h.routes)
}

// Resolve from owned session/route records for both HTTP and model tools. Input
// arguments, message text and arbitrary session metadata cannot choose a target.
func delegationSourceRoute(ctx context.Context, b botdelegation.Identity, sessions botdelegation.Sessions, routes delegationRouteReader) (botdelegation.ReturnRoute, error) {
	root, err := sessions.Get(ctx, b.SourceRootID)
	if err != nil || root.ID != b.SourceRootID || root.BotID != b.SourceBotID || root.TeamID != b.TeamID || root.CreatedByUserID != b.UserID || root.Type != thread.TypeChat || root.ParentThreadID != "" {
		return botdelegation.ReturnRoute{}, botdelegation.ErrDenied
	}
	r := botdelegation.ReturnRoute{Version: 1, Channel: root.ChannelType, ChatID: b.SourceBotID}
	// The official thread service persists Web/CLI surfaces as "local".
	// Their continuation uses the existing Web/RouteHub path, not an IM sender.
	// REST-created local threads also permit an omitted channel_type. A declared
	// external channel or any route ID must still resolve through the route store.
	if (root.ChannelType == "" || root.ChannelType == "local" || root.ChannelType == "web" || root.ChannelType == "cli") && root.RouteID == "" {
		r.Channel = "web"
		r.ReplyTarget = b.SourceBotID
		return r, nil
	}
	if root.RouteID == "" || root.ChannelType == "" || routes == nil {
		return botdelegation.ReturnRoute{}, botdelegation.ErrDenied
	}
	stored, err := routes.GetByID(ctx, root.RouteID)
	localRoute := root.ChannelType == "local" && (stored.Platform == "web" || stored.Platform == "cli")
	if err != nil || stored.ID != root.RouteID || stored.BotID != b.SourceBotID || (stored.Platform != root.ChannelType && !localRoute) || stored.ReplyTarget == "" {
		return botdelegation.ReturnRoute{}, botdelegation.ErrDenied
	}
	r.Channel = stored.Platform
	r.RouteID, r.ChannelConfigID = stored.ID, stored.ChannelConfigID
	r.ReplyTarget, r.ConversationType = stored.ReplyTarget, stored.ConversationType
	return r, nil
}

func (h *delegationHost) deliveryRoute(ctx context.Context, r botdelegation.Record) (botdelegation.ReturnRoute, error) {
	current, err := h.SourceRoute(ctx, r.Identity)
	if err != nil {
		return botdelegation.ReturnRoute{}, err
	}
	return validateDelegationReturnRoute(r.Payload.ReturnRoute, current)
}

func validateDelegationReturnRoute(saved *botdelegation.ReturnRoute, current botdelegation.ReturnRoute) (botdelegation.ReturnRoute, error) {
	if saved == nil {
		// Old Web records retain their established local-session delivery. There
		// is no safe historical external recipient to infer for other records.
		if current.Channel == "web" && current.RouteID == "" {
			return current, nil
		}
		return botdelegation.ReturnRoute{}, botdelegation.ErrDenied
	}
	if saved.Version != 1 || *saved != current {
		return botdelegation.ReturnRoute{}, botdelegation.ErrDenied
	}
	return *saved, nil
}

func delegationDeliveryCommand(r botdelegation.Record, routing botdelegation.ReturnRoute, text string, attachments []turn.Attachment) turn.StartTurnCommand {
	return turn.StartTurnCommand{
		DelegationResultID: r.ID, SchemaVersion: 1, TeamID: r.Identity.TeamID, Mode: turn.ModeChat,
		BotID: r.Identity.SourceBotID, ChatID: routing.ChatID, ThreadID: r.Identity.SourceSessionID,
		// Keep the old invocation payload stable on delivery retry. Visibility
		// follows the authorized result origin, never an empty text sentinel.
		UserID: r.Identity.UserID, SourceChannelIdentityID: r.Identity.UserID, Query: text, UserVisibleText: text, Attachments: attachments,
		SkipTitleGeneration: true, SkipMemoryExtraction: true,
		CurrentChannel: routing.Channel, RouteID: routing.RouteID, ReplyTarget: routing.ReplyTarget, ConversationType: routing.ConversationType,
	}
}

func delegationRequestMatchesRoute(req application.ChatRequest, r botdelegation.ReturnRoute) bool {
	return req.CurrentChannel == r.Channel && req.ChatID == r.ChatID && req.RouteID == r.RouteID && req.ReplyTarget == r.ReplyTarget && req.ConversationType == r.ConversationType
}
