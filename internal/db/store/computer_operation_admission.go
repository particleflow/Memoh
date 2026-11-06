package store

import "context"

// ComputerOperationAdmissionStore reserves a new dispatch on an existing
// operation. Admission and the operation revision CAS share one transaction;
// callers must not dispatch unless this method returns a committed record.
// Ordinary progress and historical result writes use SaveComputerOperation.
type ComputerOperationAdmissionStore interface {
	SaveComputerOperationWithAdmission(context.Context, ComputerOperationRecord) (ComputerOperationRecord, error)
}
