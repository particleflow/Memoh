package store

import "context"

// SessionActivityReader discovers existing persisted activity without opening
// transcripts or contacting a runtime. Permission checks stay in the projection.
type SessionActivityReader interface {
	ListSessionActivityCandidates(context.Context, SessionTaskFilter) ([]SessionTaskCandidate, error)
}
