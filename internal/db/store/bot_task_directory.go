package store

import (
	"context"
	"time"
)

// BotTaskDirectoryCandidate is discovery only. No field grants read access.
// ID is the original task ID; RecordID is its durable occurrence identity.
type BotTaskDirectoryCandidate struct {
	QueueAdmission                                        bool
	Type, ID, RecordID, SessionID, RunID                  string
	SourceBotID, SourceSessionID, ScheduleID, OwnerUserID string
	Name, State                                           string
	CreatedAt, UpdatedAt                                  time.Time
}

type BotTaskDirectoryReader interface {
	ListBotTaskDirectoryCandidates(context.Context, SessionTaskFilter) ([]BotTaskDirectoryCandidate, error)
}

// BotTaskRunReference is an exact persisted conversation run, never a latest-run
// projection or an opaque computer/provider run inferred to be a native run.
type BotTaskRunReference struct {
	BotID, SessionID, RunID, TurnID, InvocationID string
}

type BotTaskRunReader interface {
	ReadBotTaskRun(context.Context, string, string, string) (BotTaskRunReference, error)
}
