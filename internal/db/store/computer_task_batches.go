package store

import (
	"context"
	"time"
)

// ComputerTaskBatchRecord is one durable ordered queue of herdr_pi_task
// submissions. Items is the canonical ordered array; the row never stores
// transport secrets, endpoint paths or file bytes.
type ComputerTaskBatchRecord struct {
	ID, BotID, SessionID, UserID, TargetID, RunID string
	Label, Fingerprint, State                     string
	Items                                         JSON
	Delivery                                      JSON
	CreatedAt, UpdatedAt                          time.Time
}

// ComputerTaskBatchStore owns batch durability. Claim is create-or-return: the
// same (bot,session,user,run,fingerprint) replay returns the existing batch
// instead of minting a second one. Change runs the mutation while holding the
// batch row lock; the persisted write only happens when the mutation returns
// nil.
type ComputerTaskBatchStore interface {
	ClaimComputerTaskBatch(context.Context, ComputerTaskBatchRecord) (ComputerTaskBatchRecord, bool, error)
	ReadComputerTaskBatch(context.Context, string, string, string, string) (ComputerTaskBatchRecord, error)
	ListSessionComputerTaskBatches(context.Context, string, string, string, int32) ([]ComputerTaskBatchRecord, error)
	ChangeComputerTaskBatch(context.Context, string, func(*ComputerTaskBatchRecord) error) error
}

// ComputerTaskBatchQueue is the host-side view: every active batch in dispatch
// order plus the CAS-guarded aggregate result envelope.
type ComputerTaskBatchQueue interface {
	ComputerTaskBatchStore
	ListActiveComputerTaskBatches(context.Context) ([]ComputerTaskBatchRecord, error)
	ReadComputerTaskBatchDelivery(context.Context, string) ([]byte, error)
	SaveComputerTaskBatchDelivery(context.Context, string, []byte, []byte) ([]byte, error)
}
