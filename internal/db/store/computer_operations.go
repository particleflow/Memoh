package store

import (
	"context"
	"time"
)

// ComputerOperationRecord stores execution receipts, never transport secrets.
// Target membership is rechecked by the service before every read or action.
type ComputerOperationRecord struct {
	ID, BotID, SessionID, UserID, TargetID, RunID string
	Fingerprint, TargetFingerprint, State         string
	Details                                       JSON
	CancelRequested                               bool
	Revision                                      int64
	CreatedAt, UpdatedAt                          time.Time
	// DeliveryEntries is a bounded read projection, never serialized into Details.
	DeliveryEntries map[string]ComputerDeliveryEntry
}

// ComputerDeliveryEntry is one item of the existing operation delivery ledger.
// Version describes its payload, independently of the physical row layout.
type ComputerDeliveryEntry struct {
	Ordinal    int64
	DeliveryID string
	Version    int
	Data       JSON
}

// ComputerDeliveryEntryStore replaces whole-ledger rewrites with scoped rows.
// Save must atomically CAS the owning operation and persist this one entry.
type ComputerDeliveryEntryStore interface {
	ReadComputerDeliveryEntry(context.Context, ComputerOperationRecord, string) (ComputerDeliveryEntry, error)
	SaveComputerDeliveryEntry(context.Context, ComputerOperationRecord, ComputerDeliveryEntry) (ComputerOperationRecord, error)
	ListComputerDeliveryEntries(context.Context, ComputerOperationRecord, string, int) ([]ComputerDeliveryEntry, error)
}

type ComputerOperationStore interface {
	ClaimComputerOperation(context.Context, ComputerOperationRecord) (ComputerOperationRecord, bool, error)
	ReadComputerOperation(context.Context, ComputerOperationRecord) (ComputerOperationRecord, error)
	SaveComputerOperation(context.Context, ComputerOperationRecord) (ComputerOperationRecord, error)
}

// ComputerDeliveryStore changes only the bounded delivery ledger, using the
// same operation scope and revision CAS as execution receipts.
type ComputerDeliveryStore interface {
	ComputerOperationStore
	SaveComputerDelivery(context.Context, ComputerOperationRecord, JSON) (ComputerOperationRecord, error)
}

// ComputerPiHistoryStore reads prior durable continuity evidence within the
// already claimed computer slot. Implementations must retain unproven records.
type ComputerPiHistoryStore interface {
	PreviousComputerPiOperation(context.Context, ComputerOperationRecord, string) (ComputerOperationRecord, error)
}
