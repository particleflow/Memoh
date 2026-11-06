package userruntime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

type unavailableTransferStore struct {
	dbstore.RuntimeTransferStore
	err error
}

func (s unavailableTransferStore) ReadRuntimeTransfer(context.Context, string) (dbstore.RuntimeTransferRecord, error) {
	return dbstore.RuntimeTransferRecord{}, s.err
}

func TestTransferVerifierStorageFailureIsNotRevocation(t *testing.T) {
	credential := "mft_11111111-1111-4111-8111-111111111111." + strings.Repeat("a", 64)
	for _, tc := range []struct{ failure, want error }{{context.DeadlineExceeded, ErrTransferUnavailable}, {errors.New("isolated storage failure"), ErrTransferUnavailable}, {db.ErrNotFound, ErrTransferCredential}} {
		verifier := NewTransferCredentials(unavailableTransferStore{err: tc.failure}, true)
		if _, err := verifier.VerifyDevice(t.Context(), credential); !errors.Is(err, tc.want) {
			t.Fatal(err)
		}
	}
}
