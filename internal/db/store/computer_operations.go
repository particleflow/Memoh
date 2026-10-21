package store

import "context"

// ComputerOperationRecord stores execution receipts, never transport secrets.
// Target membership is rechecked by the service before every read or action.
type ComputerOperationRecord struct {
	ID, BotID, SessionID, UserID, TargetID, RunID string
	Fingerprint, TargetFingerprint, State         string
	Details                                       JSON
	CancelRequested                               bool
	Revision                                      int64
}

type ComputerOperationStore interface {
	ClaimComputerOperation(context.Context, ComputerOperationRecord) (ComputerOperationRecord, bool, error)
	ReadComputerOperation(context.Context, ComputerOperationRecord) (ComputerOperationRecord, error)
	SaveComputerOperation(context.Context, ComputerOperationRecord) (ComputerOperationRecord, error)
}
