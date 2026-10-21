package toolcontext

import (
	"context"
	"errors"
	"strings"
)

var ErrComputerBinding = errors.New("computer tools require a trusted conversation binding")

type computerBindingKey struct{}

// ComputerBinding is host-owned identity, not an MCP session ID or a model
// argument. The host establishes it after authenticating the caller and
// resolving the conversation's workspace. Agent transports carry its context;
// no Agent implementation or prompt needs to understand this type.
type ComputerBinding struct {
	BotID     string
	UserID    string
	SessionID string
	TargetID  string
	RunID     string
}

// HostComputerBinding reads process-local authority only. It is not populated
// by transport headers or tool arguments.
func HostComputerBinding(ctx context.Context) (ComputerBinding, bool) {
	if ctx == nil || !ComputerContextTrusted(ctx) {
		return ComputerBinding{}, false
	}
	b, ok := ctx.Value(computerBindingKey{}).(ComputerBinding)
	return b, ok
}

// DeriveComputerBinding is called only after the host has admitted a persisted
// child thread and verified its parent. Only the child thread/run change.
func DeriveComputerBinding(ctx context.Context, bot, parent, child, run string) (context.Context, error) {
	b, ok := HostComputerBinding(ctx)
	if !ok || ctx.Err() != nil || b.BotID != bot || b.SessionID != parent || child == "" || child == parent || run == "" {
		return ctx, ErrComputerBinding
	}
	b.SessionID, b.RunID = child, run
	return context.WithValue(ctx, computerBindingKey{}, b), nil
}

// WithComputerBinding is only for trusted host code. Child contexts cannot
// replace the identity or broaden a selected PC into native/NAS scope.
func WithComputerBinding(ctx context.Context, binding ComputerBinding) (context.Context, error) {
	if ctx == nil || ctx.Err() != nil {
		return ctx, ErrComputerBinding
	}
	binding.BotID = strings.TrimSpace(binding.BotID)
	binding.UserID = strings.TrimSpace(binding.UserID)
	binding.SessionID = strings.TrimSpace(binding.SessionID)
	binding.TargetID = strings.TrimSpace(binding.TargetID)
	binding.RunID = strings.TrimSpace(binding.RunID)
	if binding.BotID == "" || binding.UserID == "" || binding.SessionID == "" || binding.TargetID == "" {
		return ctx, ErrComputerBinding
	}
	if current, ok := ctx.Value(computerBindingKey{}).(ComputerBinding); ok {
		if current != binding {
			return ctx, ErrComputerBinding
		}
		return ctx, nil
	}
	if err := CheckComputerTarget(ctx, binding.TargetID); err != nil {
		return ctx, err
	}
	ctx = WithComputerScope(ctx, binding.TargetID)
	return context.WithValue(ctx, computerBindingKey{}, binding), nil
}

// ResolveComputerBinding fails closed for general HTTP clients, even if they
// provide valid-looking conversation IDs. A trusted host runtime supplies the
// binding in its owning run context; caller headers never establish it.
func ResolveComputerBinding(ctx context.Context, session Session) (ComputerBinding, error) { //nolint:contextcheck // Identity must come from the trusted host RunContext, not transport values.
	if session.PublicRequest {
		return ComputerBinding{}, ErrComputerBinding
	}
	if session.RunContext != nil {
		ctx = session.RunContext
	}
	if ctx == nil || ctx.Err() != nil {
		return ComputerBinding{}, ErrComputerBinding
	}
	binding, ok := ctx.Value(computerBindingKey{}).(ComputerBinding)
	if !ok || session.BotID != binding.BotID || session.SessionID != binding.SessionID {
		return ComputerBinding{}, ErrComputerBinding
	}
	if session.UserID != "" && session.UserID != binding.UserID {
		return ComputerBinding{}, ErrComputerBinding
	}
	return binding, nil
}
