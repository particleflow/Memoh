package toolcontext

import "context"

// DelegationScope is installed by the host from a persisted handoff, never
// transport headers or model arguments. Its callbacks recheck live authority.
type DelegationScope struct {
	Check   func(context.Context) error
	Target  func(context.Context, string, string, string) error
	Targets func(context.Context, string) ([]string, error)
}

type delegationScopeKey struct{}

func WithDelegationScope(ctx context.Context, scope *DelegationScope) context.Context {
	if _, ok := ctx.Value(delegationScopeKey{}).(*DelegationScope); ok {
		return ctx
	}
	return context.WithValue(ctx, delegationScopeKey{}, scope)
}

func HasDelegationScope(ctx context.Context) bool {
	s, ok := ctx.Value(delegationScopeKey{}).(*DelegationScope)
	return ok && s != nil
}

func CheckDelegation(ctx context.Context) error {
	if s, ok := ctx.Value(delegationScopeKey{}).(*DelegationScope); ok && s != nil {
		return s.Check(ctx)
	}
	return nil
}

func CheckDelegationTarget(ctx context.Context, bot, target, permission string) error {
	if s, ok := ctx.Value(delegationScopeKey{}).(*DelegationScope); ok && s != nil {
		return s.Target(ctx, bot, target, permission)
	}
	return nil
}

func DelegationTargets(ctx context.Context, bot string) ([]string, bool, error) {
	if s, ok := ctx.Value(delegationScopeKey{}).(*DelegationScope); ok && s != nil {
		ids, err := s.Targets(ctx, bot)
		return ids, true, err
	}
	return nil, false, nil
}
