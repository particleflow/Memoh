package toolcontext

import (
	"context"
	"errors"
	"strings"
)

// ErrComputerScope deliberately omits the requested target: a rejected call
// must not confirm whether another computer exists.
var ErrComputerScope = errors.New("this conversation is restricted to its selected computer")

type (
	computerScopeKey     struct{}
	untrustedComputerKey struct{}
)

func ComputerContextTrusted(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	untrusted, _ := ctx.Value(untrustedComputerKey{}).(bool)
	return !untrusted
}

// WithComputerScope binds the server-resolved conversation target. Unlike a
// workspace default, a PC scope cannot be cleared or replaced in a child
// context. Native conversations may use any target allowed by the Bot grants;
// this scope never grants access by itself.
func WithComputerScope(ctx context.Context, targetID string) context.Context {
	if ComputerScope(ctx) != "" {
		return ctx
	}
	targetID = strings.TrimSpace(targetID)
	if targetID == "" || strings.EqualFold(targetID, "native") {
		return ctx
	}
	return context.WithValue(ctx, computerScopeKey{}, targetID)
}

func ComputerScope(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	targetID, _ := ctx.Value(computerScopeKey{}).(string)
	return targetID
}

func CheckComputerTarget(ctx context.Context, targetID string) error {
	targetID = strings.TrimSpace(targetID)
	if !ComputerContextTrusted(ctx) && targetID != "" && !strings.EqualFold(targetID, "native") {
		return ErrComputerBinding
	}
	if selected := ComputerScope(ctx); selected != "" && targetID != "" && !strings.EqualFold(selected, targetID) {
		return ErrComputerScope
	}
	return nil
}
