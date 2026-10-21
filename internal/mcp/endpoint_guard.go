package mcp

import "context"

type endpointCheckKey struct{}

// WithEndpointCheck carries host policy to every actual HTTP request,
// including redirects and SSE message endpoints. It is never client metadata.
func WithEndpointCheck(ctx context.Context, check func(context.Context, string) error) context.Context {
	return context.WithValue(ctx, endpointCheckKey{}, check)
}

func CheckEndpoint(ctx context.Context, endpoint string) error {
	if check, ok := ctx.Value(endpointCheckKey{}).(func(context.Context, string) error); ok {
		return check(ctx, endpoint)
	}
	return nil
}
