package store

import (
	"context"
	"time"
)

// RuntimeTransferRecord extends the existing computer registry, never an API DTO.
type RuntimeTransferRecord struct {
	RuntimeID, UserID, Provider        string
	Config                             JSON
	CredentialID, Digest, TargetDigest string
	ExpiresAt                          time.Time
}

type RuntimeTransferStore interface {
	OwnedHTTPMCPConnectionBot(context.Context, string, string) (string, error)
	ReadOwnedRuntimeTransfer(context.Context, string, string) (RuntimeTransferRecord, error)
	ReadRuntimeTransfer(context.Context, string) (RuntimeTransferRecord, error)
	ChangeRuntimeTransfer(context.Context, RuntimeTransferRecord, RuntimeTransferRecord) (bool, error)
}
