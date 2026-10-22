package store

import (
	"context"
	"time"
)

type SessionTaskCandidate struct {
	Type, ID, SessionID string
	CreatedAt           time.Time
}

type SessionTaskFilter struct {
	BotID, UserID, ParentID string
	SnapshotAt              time.Time
	Before                  SessionTaskCandidate
	Type, ID                string
	Limit                   int32
}

// SessionTaskReader deliberately has no dispatch, observation or write methods.
type SessionTaskReader interface {
	ListSessionTaskCandidates(context.Context, SessionTaskFilter) ([]SessionTaskCandidate, error)
	ReadComputerOperation(context.Context, ComputerOperationRecord) (ComputerOperationRecord, error)
	GetPrimaryMount(context.Context, string) (BotRemoteRuntimeBindingRecord, error)
}
