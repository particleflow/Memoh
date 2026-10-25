package agenvo

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

type admissionTestStore struct {
	*agentProfileStore
	gate    dbstore.ComputerAdmission
	changes int
	failure error
}

func (s *admissionTestStore) ReadComputerAdmission(_ context.Context, _, _, _, _ string) (dbstore.ComputerAdmission, error) {
	if s.gate.Revision == 0 {
		return s.gate, db.ErrNotFound
	}
	return s.gate, nil
}

func (s *admissionTestStore) ChangeComputerAdmission(_ context.Context, owner, runtime string, raw dbstore.JSON, request string, rev int64, paused bool) (dbstore.ComputerAdmission, error) {
	if owner != "owner" || runtime != "existing" || string(raw) != string(s.config) {
		return dbstore.ComputerAdmission{}, db.ErrComputerAdmissionScope
	}
	if s.failure != nil {
		return dbstore.ComputerAdmission{}, s.failure
	}
	target, _ := DecodeTarget(raw)
	s.changes++
	s.gate = dbstore.ComputerAdmission{ConnectionID: target.ConnectionID, DeviceID: target.DeviceID, InstanceID: target.InstanceID, RequestID: request, Revision: rev + 1, Paused: paused}
	return s.gate, nil
}

func TestAdmissionOwnerBindingAndReadOnlyProbe(t *testing.T) {
	service, base, relay := serviceFixture(t)
	store := &admissionTestStore{agentProfileStore: &agentProfileStore{serviceStore: base, config: base.record.RuntimeProviderConfig}}
	service.store = store
	got, err := service.ReadAdmission(t.Context(), "owner", "existing")
	if err != nil || got.Paused || got.Revision != "0" || len(relay.calls) != 0 {
		t.Fatal(got, err, relay.calls)
	}
	paused := true
	in := ChangeAdmission{RequestID: uuid.NewString(), ExpectedRevision: "0", Paused: &paused}
	for _, kind := range []string{"owner", "missing_runtime", "credential", "request", "revision", "missing_state"} {
		t.Run(kind, func(t *testing.T) {
			candidate := in
			owner, id := "owner", "existing"
			switch kind {
			case "owner":
				owner = "other"
			case "missing_runtime":
				id = "other"
			case "credential":
				base.sourceAllowed = false
			case "request":
				candidate.RequestID = "guess"
			case "revision":
				candidate.ExpectedRevision = "-1"
			case "missing_state":
				candidate.Paused = nil
			}
			if _, err := service.SetAdmission(t.Context(), owner, id, candidate); err == nil {
				t.Fatal("unverified pause accepted")
			}
			base.sourceAllowed = true
			if store.changes != 0 || len(relay.calls) != 0 {
				t.Fatal("invalid request mutated state or contacted native")
			}
		})
	}
	got, err = service.SetAdmission(t.Context(), "owner", "existing", in)
	if err != nil || !got.Paused || got.Revision != "1" || store.changes != 1 || len(relay.calls) != 1 || relay.calls[0] != "workspace.list" {
		t.Fatal(got, err, relay.calls)
	}
	if base.created != 0 {
		t.Fatal("created runtime or credential")
	}
	store.failure = db.ErrComputerAdmissionScope
	if _, err = service.SetAdmission(t.Context(), "owner", "existing", in); !errors.Is(err, db.ErrComputerAdmissionScope) {
		t.Fatal("mapping conflict hidden", err)
	}
}
