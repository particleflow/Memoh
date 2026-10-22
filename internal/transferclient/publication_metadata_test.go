package transferclient

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
)

func metadataBareFixture(t *testing.T) *publicationMetadata {
	t.Helper()
	root, err := os.OpenRoot(configFixtureRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return &publicationMetadata{root: root, objectBytes: 64}
}

func metadataFixture(t *testing.T) *publicationMetadata {
	t.Helper()
	m := metadataBareFixture(t)
	if err := m.initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	return m
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

func TestPublicationMetadataMissingCatalogNeverMeansAbsent(t *testing.T) {
	for _, damage := range []string{"directory", "bucket", "usage", "empty-bucket"} {
		t.Run(damage, func(t *testing.T) {
			m := metadataFixture(t)
			r := publicationRecord{Version: 1, Reference: metadataReference("retained-fence"), State: "fenced"}
			appendMetadata(t, m, r)
			name := publicationKey(r.Reference) + ".journal"
			index, _ := publicationBucket(name)
			root, closeRoot, err := m.bucket(index, false)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := root.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if damage == "usage" || damage == "empty-bucket" {
				err = removeQueueFile(root, "usage")
				if err == nil && damage == "empty-bucket" {
					err = removeQueueFile(root, name)
				}
			}
			closeRoot()
			if err != nil {
				t.Fatal(err)
			}
			if damage == "directory" {
				err = m.root.RemoveAll(publicationMetadataDirectory)
			}
			if damage == "bucket" {
				parent, openErr := m.root.OpenRoot(publicationMetadataDirectory)
				if openErr != nil {
					t.Fatal(openErr)
				}
				err = parent.RemoveAll(fmt.Sprintf("%02x", index))
				_ = parent.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			// Isolated removal models storage damage; the API must never recreate an
			// empty catalog and acknowledge an absent ID or a new reservation.
			if _, f, err := m.read(t.Context(), name); err == nil {
				if f != nil {
					_ = f.Close()
				}
				t.Fatal("lost fence became absent")
			}
			if err := m.reserve(t.Context(), metadataReference("new-id"), "fenced"); err == nil {
				t.Fatal("damaged layout accepted admission")
			}
			if err := m.validate(t.Context()); err == nil {
				t.Fatal("maintenance claimed damaged layout ready")
			}
			if damage == "usage" {
				root, closeRoot, err = m.bucket(index, false)
				if err != nil {
					t.Fatal(err)
				}
				after, err := root.ReadFile(name)
				closeRoot()
				if err != nil || !bytes.Equal(raw, after) {
					t.Fatal("surviving proof was changed", err)
				}
			}
		})
	}
}

func TestPublicationMetadataMissingJournalNeverMeansAbsent(t *testing.T) {
	m := metadataFixture(t)
	r := publicationRecord{Version: 1, Reference: metadataReference("retained-single-fence"), State: "fenced"}
	appendMetadata(t, m, r)
	name := publicationKey(r.Reference) + ".journal"
	index, _ := publicationBucket(name)
	root, closeRoot, err := m.bucket(index, false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoot()
	before, err := root.ReadFile("usage")
	if err != nil {
		t.Fatal(err)
	}
	if err := removeQueueFile(root, name); err != nil {
		t.Fatal(err)
	}
	if _, f, err := m.read(t.Context(), name); !errors.Is(err, ErrStore) {
		if f != nil {
			_ = f.Close()
		}
		t.Fatal("missing retained ID became absent", err)
	}
	if err := m.reserve(t.Context(), r.Reference, "fenced"); !errors.Is(err, ErrStore) {
		t.Fatal("missing retained ID admitted again", err)
	}
	if err := m.validate(t.Context()); !errors.Is(err, ErrStore) {
		t.Fatal("missing history passed maintenance", err)
	}
	after, err := root.ReadFile("usage")
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("membership evidence changed", err)
	}
}

func TestPublicationMetadataDirtyWitnessPreservesMembership(t *testing.T) {
	for _, damage := range []string{"usage-torn", "missing-journal", "corrupt-witness"} {
		t.Run(damage, func(t *testing.T) {
			m := metadataFixture(t)
			r := publicationRecord{Version: 1, Reference: metadataReference("witness-retained"), State: "prepared", FileID: "fixture-retained-id"}
			appendMetadata(t, m, r)
			name := publicationKey(r.Reference) + ".journal"
			index, _ := publicationBucket(name)
			root, closeRoot, err := m.bucket(index, false)
			if err != nil {
				t.Fatal(err)
			}
			defer closeRoot()
			if err := m.dirty(root, index, false); err != nil {
				t.Fatal(err)
			}
			// The usage write can be torn after the witness is durable; it must
			// recover only if every previously acknowledged journal still exists.
			f, err := openPrivateFile(root, "usage", os.O_RDWR, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			err = f.Truncate(17)
			_ = f.Close()
			if err != nil {
				t.Fatal(err)
			}
			if damage == "missing-journal" {
				if err := removeQueueFile(root, name); err != nil {
					t.Fatal(err)
				}
			}
			if damage == "corrupt-witness" {
				f, err := openPrivateFile(root, "usage.dirty", os.O_RDWR, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.WriteAt([]byte{0xff}, 36)
				_ = f.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			witness, err := root.ReadFile("usage.dirty")
			if err != nil {
				t.Fatal(err)
			}
			u, err := m.usage(t.Context(), root, index)
			if damage == "usage-torn" {
				if err != nil || u.Count != 1 || len(u.Keys) != 1 || u.Keys[0] != name || u.Bytes != r.Reference.SizeBytes {
					t.Fatal("valid membership not recovered", err)
				}
				got, f, err := m.read(t.Context(), name)
				if f != nil {
					_ = f.Close()
				}
				if err != nil || got != r {
					t.Fatal("original state changed", err)
				}
			} else {
				if !errors.Is(err, ErrStore) {
					t.Fatal("damaged membership rebuilt", err)
				}
				after, err := root.ReadFile("usage.dirty")
				if err != nil || !bytes.Equal(witness, after) {
					t.Fatal("witness lost", err)
				}
				usage, err := root.ReadFile("usage")
				if err != nil || len(usage) != 17 {
					t.Fatal("damaged summary overwritten", err)
				}
			}
		})
	}
}

func TestPublicationMetadataUntrackedJournalFailsClosed(t *testing.T) {
	m := metadataFixture(t)
	r := putLegacyPublication(t, m, "untracked", "fenced")
	name := publicationKey(r.Reference) + ".journal"
	raw, err := m.root.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	index, _ := publicationBucket(name)
	root, closeRoot, err := m.bucket(index, false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoot()
	f, err := openPrivateFile(root, name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Write(raw)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, f, err := m.read(t.Context(), name); !errors.Is(err, ErrStore) {
		if f != nil {
			_ = f.Close()
		}
		t.Fatal("untracked journal accepted", err)
	}
	if err := m.dirty(root, index, false); !errors.Is(err, ErrStore) {
		t.Fatal("unexpected journal entered recovery membership", err)
	}
	after, err := root.ReadFile(name)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatal("untracked evidence changed", err)
	}
}
