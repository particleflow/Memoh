package computertask

import (
	"errors"

	"github.com/felinics/memoh/internal/db"
)

func admissionBlocked(err error) bool {
	return errors.Is(err, db.ErrComputerAdmissionPaused) || errors.Is(err, db.ErrComputerAdmissionScope)
}

func admissionErrorCode(err error) string {
	if errors.Is(err, db.ErrComputerAdmissionPaused) {
		return "computer.admission_paused"
	}
	return "computer.admission_scope_unverified"
}
