package toolcontext

import (
	"context"
	"strings"
)

type explicitWorkdirKey struct{}

// WithExplicitWorkdir preserves an existing explicit directory's precedence
// over account defaults. It conveys no path grant or execution authority.
func WithExplicitWorkdir(ctx context.Context, path string) context.Context {
	if strings.TrimSpace(path) == "" {
		return ctx
	}
	return context.WithValue(ctx, explicitWorkdirKey{}, strings.TrimSpace(path))
}

func HasExplicitWorkdir(ctx context.Context) bool {
	return ExplicitWorkdir(ctx) != ""
}

func ExplicitWorkdir(ctx context.Context) string {
	path, _ := ctx.Value(explicitWorkdirKey{}).(string)
	return path
}
