package thread

import (
	"context"

	dbstore "github.com/felinics/memoh/internal/db/store"
)

// StageRouteSession keeps normal creation validation inside the route owner's
// transaction. The returned notification must run only after a known commit;
// hooks and session-created events must not expose rolled-back sessions.
func (s *Service) StageRouteSession(ctx context.Context, queries dbstore.Queries, input CreateInput) (Thread, func(), error) {
	staged := *s
	staged.queries, staged.publisher, staged.hookService = queries, nil, nil
	created, err := staged.Create(ctx, input)
	if err != nil {
		return Thread{}, nil, err
	}
	return created, func() {
		s.publishThreadCreated(created)
		s.runThreadStartHook(context.WithoutCancel(ctx), created)
	}, nil
}
