package userruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

type unavailableTransferStore struct {
	dbstore.RuntimeTransferStore
	err error
}

type identityTransferStore struct {
	dbstore.RuntimeTransferStore
	record dbstore.RuntimeTransferRecord
	reads  int
}

func (s *identityTransferStore) ReadRuntimeTransfer(context.Context, string) (dbstore.RuntimeTransferRecord, error) {
	s.reads++
	return s.record, nil
}

// The store and credential are synthetic. This pins the existing verifier's
// purpose/identity boundary; it does not configure a native Runtime provider.
func TestTransferCredentialPurposeAndStableTuple(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	credential := "mft_" + id + "." + strings.Repeat("a", 64)
	target := agenvo.Target{ConnectionID: "22222222-2222-4222-8222-222222222222", DeviceID: "synthetic-device", InstanceID: "synthetic-instance", Session: "fixture", WorkDir: `C:\fixture`, BashPath: `C:\Git\bin\bash.exe`}
	encode := func(target agenvo.Target) []byte {
		t.Helper()
		raw, err := json.Marshal(target)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	config := encode(target)
	digest, err := transferTargetDigest(config)
	if err != nil {
		t.Fatal(err)
	}
	store := &identityTransferStore{record: dbstore.RuntimeTransferRecord{Config: config, CredentialID: id, Digest: transferDigest(credential), TargetDigest: digest}}
	verifier := NewTransferCredentials(store, true)
	for _, value := range []string{"mrk_" + credential[4:], "Bearer " + credential, "eyJhbGciOiJub25lIn0.eyJzdWIiOiJmaXh0dXJlIn0."} {
		if _, err := verifier.VerifyDevice(t.Context(), value); !errors.Is(err, ErrTransferCredential) || store.reads != 0 {
			t.Fatal("a non-file credential entered the lookup path", err, store.reads)
		}
	}
	device, err := verifier.VerifyDevice(t.Context(), credential)
	if err != nil {
		t.Fatal(err)
	}
	reopened := NewTransferCredentials(store, true)
	if again, err := reopened.VerifyDevice(t.Context(), credential); err != nil || again != device {
		t.Fatal("recreating the verifier changed the stable device", err)
	}
	for _, field := range []string{"connection", "device", "instance"} {
		t.Run(field, func(t *testing.T) {
			changed := target
			switch field {
			case "connection":
				changed.ConnectionID = "33333333-3333-4333-8333-333333333333"
			case "device":
				changed.DeviceID = "other-device"
			case "instance":
				changed.InstanceID = "other-instance"
			}
			store.record.Config = encode(changed)
			store.record.TargetDigest = digest
			if _, err := reopened.VerifyDevice(t.Context(), credential); !errors.Is(err, ErrTransferCredential) {
				t.Fatal("config mutation silently rebound the file credential", err)
			}
			// Even an already-updated record cannot revalidate an in-flight
			// principal for the old tuple. This is not an enrollment/migration.
			store.record.TargetDigest, err = transferTargetDigest(store.record.Config)
			if err != nil {
				t.Fatal(err)
			}
			if err := reopened.RevalidateDevice(t.Context(), device); !errors.Is(err, ErrTransferCredential) {
				t.Fatal("changed tuple retained the old principal", err)
			}
			store.record.Config, store.record.TargetDigest = config, digest
		})
	}
	if err := reopened.RevalidateDevice(t.Context(), device); err != nil {
		t.Fatal("the original tuple no longer revalidates", err)
	}
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
