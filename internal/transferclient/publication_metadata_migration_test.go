package transferclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
)

func putLegacyPublication(t *testing.T, m *publicationMetadata, id, state string) publicationRecord {
	t.Helper()
	r := publicationRecord{Version: 1, Reference: metadataReference(id), State: state}
	if state != "fenced" {
		r.State = "prepared"
		r.FileID = "fixture-identity-" + id
	}
	f, err := openPrivateFile(m.root, publicationKey(r.Reference)+".journal", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(r)
	err = appendJournalFrame(f, raw, publicationJournalBytes)
	if err == nil && state == "published" {
		r.State = state
		raw, _ = json.Marshal(r)
		err = appendJournalFrame(f, raw, publicationJournalBytes)
	}
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPublicationMetadataMigrationPreservesCompleteHistory(t *testing.T) {
	m := metadataBareFixture(t)
	m.objectBytes = 4096
	records := make([]publicationRecord, 20)
	original := map[string][]byte{}
	backup, err := os.OpenRoot(configFixtureRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = backup.Close() }()
	for i := range records {
		r := putLegacyPublication(t, m, fmt.Sprintf("legacy-%02d", i), []string{"fenced", "prepared", "published"}[i%3])
		records[i] = r
		name := publicationKey(r.Reference) + ".journal"
		raw, err := m.root.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		original[name] = bytes.Clone(raw)
		f, err := openPrivateFile(backup, name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.Write(raw)
		if err == nil {
			err = f.Sync()
		}
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
		// Multiple matching prefixes can coexist after finite migration interruption.
		if i < 2 {
			index, _ := publicationBucket(name)
			root, closeRoot, err := m.bucket(index, true)
			if err != nil {
				t.Fatal(err)
			}
			if err := m.dirty(root, index, true); err != nil {
				t.Fatal(err)
			}
			f, err := openPrivateFile(root, name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			_, err = f.Write(raw[:41])
			_ = f.Close()
			closeRoot()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	done := false
	for round := 0; round < 8 && !done; round++ {
		done, err = m.migrateLegacy(t.Context())
		if err != nil {
			t.Fatal(err)
		}
	}
	if !done {
		t.Fatal("bounded calls did not finish")
	}
	for _, want := range records {
		name := publicationKey(want.Reference) + ".journal"
		got, f, err := m.read(t.Context(), name)
		if f != nil {
			_ = f.Close()
		}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("state changed", err)
		}
		index, _ := publicationBucket(name)
		root, closeRoot, err := m.bucket(index, false)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := root.ReadFile(name)
		closeRoot()
		if err != nil || !bytes.Equal(raw, original[name]) {
			t.Fatal("raw journal changed", err)
		}
		raw, err = backup.ReadFile(name)
		if err != nil || !bytes.Equal(raw, original[name]) {
			t.Fatal("original-layout backup changed", err)
		}
		if _, err := m.root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("old copy not removed after durable target", err)
		}
	}
	done, err = m.migrateLegacy(t.Context())
	if err != nil || !done {
		t.Fatal("repeated maintenance", err)
	}
}

func TestPublicationMetadataMigrationConflictPreservesBothCopies(t *testing.T) {
	m := metadataBareFixture(t)
	r := putLegacyPublication(t, m, "conflict", "fenced")
	name := publicationKey(r.Reference) + ".journal"
	raw, err := m.root.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	index, _ := publicationBucket(name)
	root, closeRoot, err := m.bucket(index, true)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoot()
	f, err := openPrivateFile(root, name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	bad := bytes.Clone(raw)
	bad[len(bad)-1] ^= 1
	_, err = f.Write(bad)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.migrateLegacy(t.Context()); !errors.Is(err, ErrPublicationConflict) {
		t.Fatal("conflict accepted", err)
	}
	old, err := m.root.ReadFile(name)
	if err != nil || !bytes.Equal(old, raw) {
		t.Fatal("old evidence lost", err)
	}
	target, err := root.ReadFile(name)
	if err != nil || !bytes.Equal(target, bad) {
		t.Fatal("conflict evidence lost", err)
	}
}

func TestPublicationMetadataMigrationInterruptedCopies(t *testing.T) {
	for _, stage := range []string{"publication_before_target_sync", "publication_after_target_sync", "publication_before_legacy_remove", "publication_after_legacy_remove"} {
		t.Run(stage, func(t *testing.T) {
			m := metadataBareFixture(t)
			r := putLegacyPublication(t, m, "interrupted-migration", "published")
			sentinel := "test boundary"
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
				_, _ = m.migrateLegacy(t.Context())
			}()
			m.hook = nil
			done, err := m.migrateLegacy(t.Context())
			if err != nil || !done {
				t.Fatal("recovery failed", done, err)
			}
			got, f, err := m.read(t.Context(), publicationKey(r.Reference)+".journal")
			if f != nil {
				_ = f.Close()
			}
			if err != nil || !reflect.DeepEqual(got, r) {
				t.Fatal("state lost", err)
			}
		})
	}
}

func TestPublicationMetadataMigrationMissingDurableTarget(t *testing.T) {
	m := metadataBareFixture(t)
	var names []string
	for i := range 20 {
		r := putLegacyPublication(t, m, fmt.Sprintf("missing-target-%02d", i), "fenced")
		names = append(names, publicationKey(r.Reference)+".journal")
	}
	// First call copies sixteen; second makes every target durable and removes
	// sixteen originals. Damage after that checkpoint must not be hidden by the
	// cached count when migration resumes, nor consume the remaining originals.
	for range 2 {
		if done, err := m.migrateLegacy(t.Context()); err != nil || done {
			t.Fatal("finite checkpoint", done, err)
		}
	}
	var removed string
	var remaining []string
	for _, name := range names {
		if _, err := m.root.Lstat(name); errors.Is(err, os.ErrNotExist) {
			removed = name
		} else if err != nil {
			t.Fatal(err)
		} else {
			remaining = append(remaining, name)
		}
	}
	if removed == "" || len(remaining) != 4 {
		t.Fatal("checkpoint did not retain four originals")
	}
	index, _ := publicationBucket(removed)
	root, closeRoot, err := m.bucket(index, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := removeQueueFile(root, removed); err != nil {
		t.Fatal(err)
	}
	closeRoot()
	if done, err := m.migrateLegacy(t.Context()); done || !errors.Is(err, ErrStore) {
		t.Fatal("lost durable target claimed migration complete", done, err)
	}
	for _, name := range remaining {
		if _, err := m.root.Lstat(name); err != nil {
			t.Fatal("remaining original removed after evidence loss", err)
		}
	}
}
