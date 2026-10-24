package computertask

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
)

// collectBatchRead is called only by explicit result/log collection. Read-only
// status/observation handlers never reserve or create a reader. The original
// Progress, receipt and file offsets are not replaced by this resource record.
func (s *Service) collectBatchRead(ctx context.Context, scope agenvo.Scope, batch *BatchDetails, save func() error, read func(*agenvo.BatchRecoveryReader) error) error {
	if batch.RecoveryReader != nil && batch.RecoveryReader.Phase != "ready" {
		return agenvo.ErrUnknown // includes restart with creating: never create again
	}
	err := read(batch.RecoveryReader)
	if batch.RecoveryReader != nil || !errors.Is(err, agenvo.ErrBatchControlChanged) {
		return err
	}
	reservation := &agenvo.BatchRecoveryReader{RequestID: uuid.NewString(), Phase: "creating"}
	batch.RecoveryReader = reservation
	if err := save(); err != nil {
		return err // only the durable CAS winner can issue workspace.create
	}
	ready, err := s.relay.CreateBatchRecoveryReader(ctx, scope, batch.Progress, *reservation)
	if err != nil {
		return agenvo.ErrUnknown // known/unknown failures both retain the one reservation
	}
	batch.RecoveryReader = &ready
	if err := save(); err != nil {
		batch.RecoveryReader = reservation
		return err // never type into a resource whose ownership was not committed
	}
	return read(&ready)
}
