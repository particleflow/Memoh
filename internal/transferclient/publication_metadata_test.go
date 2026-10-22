package transferclient

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
)

func metadataFixture(t *testing.T) *publicationMetadata {
	t.Helper()
	root, err := os.OpenRoot(configFixtureRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return &publicationMetadata{root: root, objectBytes: 64}
}

func metadataReference(id string) Reference {
	j := newJob([]byte("fixture"))
	return Reference{DeliveryID: id, OperationID: j.OperationID, BotID: j.BotID, ContentHash: j.Spec.SHA256, SizeBytes: j.Spec.SizeBytes, Mime: j.Spec.Mime}
}

func appendMetadata(t *testing.T, m *publicationMetadata, r publicationRecord) {
	t.Helper()
	if err := m.reserve(t.Context(), r.Reference, r.State); err != nil {
		t.Fatal(err)
	}
	f, err := m.create(r.Reference)
	if err != nil {
		t.Fatal(err)
	}
	err = m.save(t.Context(), f, r)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestPublicationMetadataBeyond128AndReplay(t *testing.T) {
	m := metadataFixture(t)
	for i := range 160 {
		r := publicationRecord{Version: 1, Reference: metadataReference(fmt.Sprintf("metadata-%03d", i)), State: "fenced"}
		appendMetadata(t, m, r)
	}
	reopened := &publicationMetadata{root: m.root, objectBytes: m.objectBytes}
	for i := range 160 {
		want := publicationRecord{Version: 1, Reference: metadataReference(fmt.Sprintf("metadata-%03d", i)), State: "fenced"}
		got, f, err := reopened.read(t.Context(), publicationKey(want.Reference)+".journal")
		if f != nil {
			_ = f.Close()
		}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("replay identity", i, err)
		}
	}
	// Fenced history has zero object bytes but still consumes metadata capacity.
	if err := reopened.reserve(t.Context(), metadataReference("more"), "prepared"); err != nil {
		t.Fatal(err)
	}
}

func TestPublicationMetadataByteBudgetAndStateTransitions(t *testing.T) {
	m := metadataFixture(t)
	m.objectBytes = 7
	r := publicationRecord{Version: 1, Reference: metadataReference("object"), State: "prepared", FileID: "fixture-file-id"}
	appendMetadata(t, m, r)
	if err := m.reserve(t.Context(), metadataReference("another"), "prepared"); !errors.Is(err, ErrCapacity) {
		t.Fatal("object byte cap", err)
	}
	got, f, err := m.read(t.Context(), publicationKey(r.Reference)+".journal")
	if err != nil {
		t.Fatal(err)
	}
	got.State = "fenced"
	err = m.save(t.Context(), f, got)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := m.reserve(t.Context(), metadataReference("another"), "prepared"); err != nil {
		t.Fatal("fence did not release reservation", err)
	}
	replay, f, err := m.read(t.Context(), publicationKey(r.Reference)+".journal")
	if f != nil {
		_ = f.Close()
	}
	if err != nil || !reflect.DeepEqual(replay, got) {
		t.Fatal("tombstone lost", err)
	}
}

func TestPublicationMetadataDirtyRecovery(t *testing.T) {
	for _, stage := range []string{"metadata_after_dirty", "metadata_after_journal_sync", "metadata_after_usage_sync", "metadata_after_clean"} {
		t.Run(stage, func(t *testing.T) {
			m := metadataFixture(t)
			r := publicationRecord{Version: 1, Reference: metadataReference("interrupted"), State: "prepared", FileID: "source-id"}
			appendMetadata(t, m, r)
			_, f, err := m.read(t.Context(), publicationKey(r.Reference)+".journal")
			if err != nil {
				t.Fatal(err)
			}
			// Stop at exact durable boundaries, then reopen the same authoritative files.
			sentinel := "injected process boundary"
			m.hook = func(at string) {
				if at == stage {
					panic(sentinel)
				}
			}
			func() {
				defer func() {
					if recover() != sentinel {
						t.Error("hook not reached")
					}
				}()
				r.State = "published"
				_ = m.save(t.Context(), f, r)
			}()
			_ = f.Close()
			m.hook = nil
			got, f, err := m.read(t.Context(), publicationKey(r.Reference)+".journal")
			if f != nil {
				_ = f.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			want := "published"
			if stage == "metadata_after_dirty" {
				want = "prepared"
			}
			if got.State != want || got.Reference != r.Reference || got.FileID != r.FileID {
				t.Fatal("lost state", got.State)
			}
			if err := m.reserve(t.Context(), metadataReference("probe"), "fenced"); err != nil {
				t.Fatal("cannot resume capacity", err)
			}
		})
	}
}

func TestPublicationMetadataUnknownAndCorruptionPreserveEvidence(t *testing.T) {
	for _, kind := range []string{"empty-journal", "corrupt-frame", "corrupt-usage"} {
		t.Run(kind, func(t *testing.T) {
			m := metadataFixture(t)
			r := publicationRecord{Version: 1, Reference: metadataReference("unknown"), State: "fenced"}
			if kind == "empty-journal" {
				if err := m.reserve(t.Context(), r.Reference, r.State); err != nil {
					t.Fatal(err)
				}
				f, err := m.create(r.Reference)
				if err != nil {
					t.Fatal(err)
				}
				_ = f.Close()
			} else {
				appendMetadata(t, m, r)
			}
			name := publicationKey(r.Reference) + ".journal"
			index, _ := publicationBucket(name)
			root, closeRoot, err := m.bucket(index, false)
			if err != nil {
				t.Fatal(err)
			}
			defer closeRoot()
			target := name
			if kind == "corrupt-usage" {
				target = "usage"
			}
			if kind != "empty-journal" {
				f, err := openPrivateFile(root, target, os.O_RDWR, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.WriteAt([]byte{0xff}, 36)
				_ = f.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			before, err := root.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if _, f, err := m.read(t.Context(), name); err == nil {
				if f != nil {
					_ = f.Close()
				}
				t.Fatal("damaged evidence accepted")
			}
			after, err := root.ReadFile(target)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("evidence overwritten", err)
			}
			// Full-frame corruption needs inspection, never a guessed empty reservation.
		})
	}
}
