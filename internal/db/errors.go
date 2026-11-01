package db

import "errors"

var (
	ErrNotFound                  = errors.New("database record not found")
	ErrLastActiveAdmin           = errors.New("team must retain at least one active admin")
	ErrComputerAdmissionPaused   = errors.New("computer admission is paused")
	ErrComputerAdmissionConflict = errors.New("computer admission revision conflicts")
	ErrComputerAdmissionScope    = errors.New("computer admission scope is unverified")
	// ErrAuthSessionConflict means a login still collided with a concurrent
	// login on the same device after its one transaction retry.
	ErrAuthSessionConflict = errors.New("auth session device conflict")
	// ErrCommitOutcomeUnknown means a transaction's COMMIT acknowledgement was
	// lost. Callers that would perform destructive compensation must reconcile
	// an idempotency/publication key on a fresh connection first.
	ErrCommitOutcomeUnknown = errors.New("database transaction commit outcome is unknown")
)
