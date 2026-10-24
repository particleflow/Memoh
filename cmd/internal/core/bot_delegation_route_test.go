package core

import (
	"context"
	"errors"
	"testing"

	"github.com/felinics/memoh/internal/agent/application"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/channel/route"
	"github.com/felinics/memoh/internal/chat/thread"
)

type delegationRouteSession struct{ value thread.Thread }

func (s delegationRouteSession) Get(context.Context, string) (thread.Thread, error) {
	return s.value, nil
}

type delegationStoredRoute struct{ value route.Route }

func (s delegationStoredRoute) GetByID(context.Context, string) (route.Route, error) {
	return s.value, nil
}

func TestDelegationHostRestoresOriginalRoute(t *testing.T) {
	b := botdelegation.Identity{TeamID: "team", UserID: "user", SourceBotID: "manager", SourceSessionID: "child", SourceRootID: "root"}
	root := thread.Thread{ID: "root", TeamID: "team", BotID: "manager", CreatedByUserID: "user", Type: thread.TypeChat, ChannelType: "telegram", RouteID: "route"}
	stored := route.Route{ID: "route", BotID: "manager", Platform: "telegram", ReplyTarget: "synthetic-chat", ChannelConfigID: "config", ConversationType: "group"}
	routing, err := delegationSourceRoute(t.Context(), b, delegationRouteSession{root}, delegationStoredRoute{stored})
	if err != nil {
		t.Fatal(err)
	}
	r := botdelegation.Record{ID: "delegation", Identity: b, Payload: botdelegation.Payload{ReturnRoute: &routing}}
	cmd := delegationDeliveryCommand(r, routing, "final", nil)
	if cmd.CurrentChannel != "telegram" || cmd.RouteID != "route" || cmd.ReplyTarget != "synthetic-chat" || cmd.ThreadID != "child" || cmd.ChatID != "manager" {
		t.Fatalf("lost original routing: %+v", cmd)
	}
	req := application.ChatRequest{CurrentChannel: cmd.CurrentChannel, ChatID: cmd.ChatID, RouteID: cmd.RouteID, ReplyTarget: cmd.ReplyTarget, ConversationType: cmd.ConversationType}
	if !delegationRequestMatchesRoute(req, routing) {
		t.Fatal("valid continuation rejected")
	}
	for _, change := range []string{"channel", "route", "recipient", "chat"} {
		t.Run(change, func(t *testing.T) {
			forged := req
			switch change {
			case "channel":
				forged.CurrentChannel = "web"
			case "route":
				forged.RouteID = "other"
			case "recipient":
				forged.ReplyTarget = "other"
			case "chat":
				forged.ChatID = "other"
			}
			if delegationRequestMatchesRoute(forged, routing) {
				t.Fatal("forged queued recipient accepted")
			}
		})
	}
	for _, change := range []string{"bot", "owner", "team", "route", "platform", "empty-recipient"} {
		t.Run(change, func(t *testing.T) {
			s, rt := root, stored
			switch change {
			case "bot":
				rt.BotID = "other"
			case "owner":
				s.CreatedByUserID = "other"
			case "team":
				s.TeamID = "other"
			case "route":
				rt.ID = "other"
			case "platform":
				rt.Platform = "other"
			case "empty-recipient":
				rt.ReplyTarget = ""
			}
			if _, err := delegationSourceRoute(t.Context(), b, delegationRouteSession{s}, delegationStoredRoute{rt}); !errors.Is(err, botdelegation.ErrDenied) {
				t.Fatalf("untrusted route accepted: %v", err)
			}
		})
	}
}

func TestDelegationLegacyAndChangedReturnRoutes(t *testing.T) {
	web := botdelegation.ReturnRoute{Version: 1, Channel: "web", ChatID: "manager", ReplyTarget: "manager"}
	if _, err := validateDelegationReturnRoute(nil, web); err != nil {
		t.Fatal("legacy Web record no longer deliverable")
	}
	external := web
	external.Channel, external.RouteID, external.ReplyTarget = "telegram", "route", "synthetic-chat"
	if _, err := validateDelegationReturnRoute(nil, external); !errors.Is(err, botdelegation.ErrDenied) {
		t.Fatal("legacy external recipient guessed")
	}
	if _, err := validateDelegationReturnRoute(&external, external); err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"recipient", "route", "channel-config", "version"} {
		t.Run(change, func(t *testing.T) {
			current := external
			switch change {
			case "recipient":
				current.ReplyTarget = "new-chat"
			case "route":
				current.RouteID = "other"
			case "channel-config":
				current.ChannelConfigID = "other"
			case "version":
				current.Version = 2
			}
			if _, err := validateDelegationReturnRoute(&external, current); !errors.Is(err, botdelegation.ErrDenied) {
				t.Fatal("route change silently redirected old delivery")
			}
		})
	}
}

func TestDelegationLocalChannelCompatibility(t *testing.T) {
	b := botdelegation.Identity{TeamID: "team", UserID: "user", SourceBotID: "manager", SourceSessionID: "root", SourceRootID: "root"}
	for _, channel := range []string{"", "local", "web", "cli"} {
		t.Run(channel, func(t *testing.T) {
			root := thread.Thread{ID: "root", TeamID: "team", BotID: "manager", CreatedByUserID: "user", Type: thread.TypeChat, ChannelType: channel}
			r, err := delegationSourceRoute(t.Context(), b, delegationRouteSession{root}, nil)
			if err != nil || r.Channel != "web" || r.ReplyTarget != "manager" || r.RouteID != "" {
				t.Fatalf("local continuation lost: %+v %v", r, err)
			}
			if _, err := validateDelegationReturnRoute(nil, r); err != nil {
				t.Fatal("legacy local record rejected")
			}
		})
	}
}
