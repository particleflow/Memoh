package core

import (
	"context"
	"errors"
	"testing"

	tools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/toolcontext"
)

type imagePermissionProbe struct {
	allowed bool
	calls   int
}

func (p *imagePermissionProbe) HasBotPermission(_ context.Context, bot, user, permission string) (bool, error) {
	p.calls++
	return p.allowed && bot == "bot" && user == "user" && permission == bots.PermissionChat, nil
}

func TestImageCapabilityUsesCurrentBotAndSessionAuthority(t *testing.T) {
	p := &imagePermissionProbe{allowed: true}
	get := func(_ context.Context, id string) (thread.Thread, error) {
		if id != "session" {
			return thread.Thread{}, errors.New("missing session")
		}
		return thread.Thread{BotID: "bot"}, nil
	}
	authorize := imageCapabilityAuthorizer(p, get)
	session := tools.SessionContext{BotID: "bot", UserID: "user", SessionID: "session"}
	if err := authorize(t.Context(), session); err != nil {
		t.Fatal(err)
	}
	p.allowed = false
	if err := authorize(t.Context(), session); err == nil {
		t.Fatal("revoked Bot access accepted")
	}
	p.allowed = true
	for _, alter := range []func(*tools.SessionContext){func(s *tools.SessionContext) { s.BotID = "other" }, func(s *tools.SessionContext) { s.UserID = "other" }, func(s *tools.SessionContext) { s.SessionID = "other" }} {
		changed := session
		alter(&changed)
		if authorize(t.Context(), changed) == nil {
			t.Fatal("identity mismatch accepted")
		}
	}
	wrongBot := imageCapabilityAuthorizer(p, func(context.Context, string) (thread.Thread, error) { return thread.Thread{BotID: "other"}, nil })
	if wrongBot(t.Context(), session) == nil {
		t.Fatal("session from another Bot accepted")
	}
	ctx := toolcontext.WithDelegationScope(t.Context(), &toolcontext.DelegationScope{Check: func(context.Context) error { return errors.New("revoked delegation") }})
	if authorize(ctx, session) == nil {
		t.Fatal("delegation revocation bypassed")
	}
}
