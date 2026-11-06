package schedule

import (
	"bytes"
	"context"
)

func hasResultOrigin(raw []byte) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && !bytes.Equal(raw, []byte("{}"))
}

// Set only by the product tool adapter after resolving the authenticated contact.
// It is deliberately absent from public CreateRequest / tool JSON schemas.
type resultOriginKey struct{}

func WithResultOrigin(ctx context.Context, origin []byte) context.Context {
	return context.WithValue(ctx, resultOriginKey{}, append([]byte(nil), origin...))
}

func resultOrigin(ctx context.Context) []byte {
	value, _ := ctx.Value(resultOriginKey{}).([]byte)
	return value
}

type ResultDeliveryHooks interface {
	AuthorizeScheduleResultOrigin(context.Context, string, []byte) (string, error)
	WakeScheduleResults(context.Context)
}

func (s *Service) SetResultDeliveryHooks(hooks ResultDeliveryHooks) { s.resultHooks = hooks }
