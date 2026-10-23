package store

import (
	"context"
	"encoding/json"
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

// SubagentRun contains only the current generation's own latest durable run.
// Content is the bounded last visible message from that exact run, not logs.
type SubagentRun struct {
	BotID, SessionID, RunID, State string
	UpdatedAt                      time.Time
	Content                        json.RawMessage
}

// SessionTaskReader deliberately has no dispatch, observation or write methods.
type SessionTaskReader interface {
	ListSessionTaskCandidates(context.Context, SessionTaskFilter) ([]SessionTaskCandidate, error)
	ReadComputerOperation(context.Context, ComputerOperationRecord) (ComputerOperationRecord, error)
	GetPrimaryMount(context.Context, string) (BotRemoteRuntimeBindingRecord, error)
	ReadSubagentRun(context.Context, string, string) (SubagentRun, error)
}
