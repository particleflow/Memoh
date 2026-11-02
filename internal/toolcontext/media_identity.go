package toolcontext

import "context"

// MediaIdentity is the conversation a running tool belongs to: the Bot that
// owns the session, the user it runs for, and the session itself. It is set by
// the host from admission, never from tool arguments or transport headers, so a
// media read can re-authorize the same conversation that is making the call.
type MediaIdentity struct {
	BotID     string
	UserID    string
	SessionID string
}

type mediaIdentityKey struct{}

// WithMediaIdentity records the running tool's conversation. An incomplete
// identity is dropped: without all three parts no media authorization can be
// rechecked, and a partial one would only look authoritative.
func WithMediaIdentity(ctx context.Context, identity MediaIdentity) context.Context {
	if identity.BotID == "" || identity.UserID == "" || identity.SessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, mediaIdentityKey{}, identity)
}

// MediaIdentityFrom returns the conversation recorded by WithMediaIdentity.
func MediaIdentityFrom(ctx context.Context) (MediaIdentity, bool) {
	identity, ok := ctx.Value(mediaIdentityKey{}).(MediaIdentity)
	if !ok {
		return MediaIdentity{}, false
	}
	return identity, true
}
