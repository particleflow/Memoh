package store

import (
	"context"
	"time"
)

// BotTaskDirectoryCandidate is discovery only. No field grants read access.
// ID is the original task ID; RecordID is its durable occurrence identity.
type BotTaskDirectoryCandidate struct {
	Type, ID, RecordID, SessionID, RunID                  string
	SourceBotID, SourceSessionID, ScheduleID, OwnerUserID string
	Name, State                                           string
	CreatedAt, UpdatedAt                                  time.Time
}

type BotTaskDirectoryReader interface {
	ListBotTaskDirectoryCandidates(context.Context, SessionTaskFilter) ([]BotTaskDirectoryCandidate, error)
}
