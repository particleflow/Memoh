//go:build windows

package transferclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func backupNativePublicationLayout(t *testing.T, source *os.Root) func() {
	t.Helper()
	backup, err := os.OpenRoot(nativeFixtureRoot(t, false))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backup.Close() })
	expected := map[string][]byte{}
	var copyTree func(*os.Root, *os.Root, string)
	copyTree = func(src, dst *os.Root, prefix string) {
		dir, err := src.Open(".")
		if err != nil {
			t.Fatal(err)
		}
		entries, err := dir.ReadDir(-1)
		_ = dir.Close()
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.Type()&os.ModeSymlink != 0 {
				t.Fatal("backup fixture contains link")
			}
			if entry.IsDir() {
				if err := makeQueueDir(dst, name); err != nil {
					t.Fatal(err)
				}
				s, err := src.OpenRoot(name)
				if err != nil {
					t.Fatal(err)
				}
				d, err := dst.OpenRoot(name)
				if err != nil {
					t.Fatal(err)
				}
				copyTree(s, d, prefix+name+"/")
				_ = s.Close()
				_ = d.Close()
				continue
			}
			raw, err := src.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			expected[prefix+name] = bytes.Clone(raw)
			f, err := openPrivateFile(dst, name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
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
		}
	}
	copyTree(source, backup, "")
	return func() {
		for name, want := range expected {
			got, err := backup.ReadFile(name)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatal("raw original-layout backup changed", name, err)
			}
		}
	}
}

func legacyNativePublication(t *testing.T, extra int) (*DiskQueue, Reference, publicationRecord, func()) {
	t.Helper()
	q := nativeQueue(t, nativeFixtureRoot(t, false))
	ref, source := publicationFixtureSource(t, q, []byte("fixture"))
	p, err := q.OpenNativePublication(func(ctx context.Context, _ Reference) error { return ctx.Err() })
	if err != nil {
		t.Fatal(err)
	}
	if out, err := p.Publish(t.Context(), ref, source); err != nil || out != OutcomeCommitted {
		t.Fatal(out, err)
	}
	source.active = false
	_ = source.file.Close()
	r, f, err := p.read(t.Context(), publicationKey(ref)+".journal")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	old, err := openPrivateFile(p.root, publicationKey(ref)+".journal", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.Write(raw)
	if err == nil {
		err = old.Sync()
	}
	_ = old.Close()
	if err != nil {
		t.Fatal(err)
	}
	// Fixture-only conversion builds the exact old flat layout. It never removes
	// an object, user file, live production tree, or any failed-migration evidence.
	if err := p.root.RemoveAll(publicationMetadataDirectory); err != nil {
		t.Fatal(err)
	}
	for i := range extra {
		putLegacyPublication(t, p.metadata, fmt.Sprintf("old-fence-%02d", i), "fenced")
	}
	prepared := metadataReference("old-prepared")
	part, err := q.file(prepared.DeliveryID+".part", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = part.Write([]byte("fixture"))
	if err == nil {
		err = part.Sync()
	}
	identity, idErr := queueFileIdentity(part)
	_ = part.Close()
	if err != nil || idErr != nil {
		t.Fatal(err, idErr)
	}
	preparedRecord := publicationRecord{Version: 1, Reference: prepared, State: "prepared", FileID: identity}
	f, err = openPrivateFile(p.root, publicationKey(prepared)+".journal", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(preparedRecord)
	err = appendJournalFrame(f, raw, publicationJournalBytes)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	f, err = openPrivateFile(p.root, "locator", os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteAt([]byte{'1'}, int64(len(`{"version":`)))
	if err == nil {
		err = f.Sync()
	}
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	_ = p.Close()
	verify := backupNativePublicationLayout(t, q.root)
	return q, ref, r, verify
}

func finishPublicationMigration(t *testing.T, q *DiskQueue) {
	t.Helper()
	for range 20 {
		done, err := q.MigrateNativePublication(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if done {
			return
		}
	}
	t.Fatal("finite migration did not finish")
}

func verifyMigratedNativePublication(t *testing.T, q *DiskQueue, ref Reference, want publicationRecord) {
	t.Helper()
	p, err := q.OpenNativePublication(func(ctx context.Context, _ Reference) error { return ctx.Err() })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	got, f, err := p.read(t.Context(), publicationKey(ref)+".journal")
	if f != nil {
		_ = f.Close()
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("publication identity changed", err)
	}
	reader, err := p.OpenPublished(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || !bytes.Equal(raw, []byte("fixture")) {
		t.Fatal("published bytes lost", err)
	}
	if out, err := p.Resolve(t.Context(), ref, true); err != nil || out != OutcomeCommitted {
		t.Fatal("late cancel retracted committed object", out, err)
	}
	prepared := metadataReference("old-prepared")
	if out, err := p.Resolve(t.Context(), prepared, false); err != nil || out != OutcomeAbsent {
		t.Fatal("prepared replay", out, err)
	}
	if out, err := p.Resolve(t.Context(), prepared, true); err != nil || out != OutcomeCanceled {
		t.Fatal("prepared fence", out, err)
	}
	if _, err := q.root.Lstat(queueName(prepared.DeliveryID + ".part")); err != nil {
		t.Fatal("migration deleted prepared payload", err)
	}
}

func TestWindowsNativePublicationExplicitMigration(t *testing.T) {
	q, ref, r, verifyBackup := legacyNativePublication(t, 20)
	authorize := func(ctx context.Context, _ Reference) error { return ctx.Err() }
	if p, err := q.OpenNativePublication(authorize); !errors.Is(err, ErrVersion) {
		if p != nil {
			_ = p.Close()
		}
		t.Fatal("implicit old layout acceptance", err)
	}
	done, err := q.MigrateNativePublication(t.Context())
	if err != nil || done {
		t.Fatal("expected finite pending migration", done, err)
	}
	if p, err := q.OpenNativePublication(authorize); !errors.Is(err, ErrVersion) {
		if p != nil {
			_ = p.Close()
		}
		t.Fatal("partial migration allowed writer", err)
	}
	finishPublicationMigration(t, q)
	finishPublicationMigration(t, q)
	verifyMigratedNativePublication(t, q, ref, r)
	verifyBackup()
	p, err := q.OpenNativePublication(authorize)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	for i := range 20 {
		if out, err := p.Resolve(t.Context(), metadataReference(fmt.Sprintf("old-fence-%02d", i)), false); err != nil || out != OutcomeCanceled {
			t.Fatal("old tombstone", out, err)
		}
	}
	oldLocator, err := p.root.ReadFile("locator-v1.backup")
	if err != nil || !bytes.Contains(oldLocator, []byte(`"version":1`)) {
		t.Fatal("old locator not retained", err)
	}
	current, err := p.root.ReadFile("locator")
	if err != nil || !bytes.Contains(current, []byte(`"version":2`)) || bytes.Equal(oldLocator, current) {
		t.Fatal("old reader version gate", err)
	}
}

func TestWindowsNativePublicationMigrationProcessCrash(t *testing.T) {
	if path := os.Getenv("MEMOH_PUBLICATION_MIGRATION_ROOT"); path != "" {
		q := nativeQueue(t, path)
		p, err := q.openNativePublication(func(ctx context.Context, _ Reference) error { return ctx.Err() }, true, func(stage string) {
			if stage == os.Getenv("MEMOH_PUBLICATION_MIGRATION_STAGE") {
				os.Exit(86)
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = p.Close() }()
		if _, err := p.migratePublication(t.Context()); err != nil {
			t.Fatal(err)
		}
		t.Fatal("crash hook not reached")
	}
	for _, stage := range []string{"publication_after_locator_backup", "publication_after_migration_gate", "publication_before_target_sync", "publication_after_target_sync", "metadata_after_usage_sync", "publication_before_legacy_remove", "publication_after_legacy_remove", "publication_before_locator_commit", "publication_after_locator_commit"} {
		t.Run(stage, func(t *testing.T) {
			q, ref, r, verifyBackup := legacyNativePublication(t, 1)
			path := q.root.Name()
			state := filepath.Dir(path)
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestWindowsNativePublicationMigrationProcessCrash$") //nolint:gosec // Current isolated test executable and fixed action.
			cmd.Env = append(os.Environ(), "MEMOH_PUBLICATION_MIGRATION_ROOT="+state, "MEMOH_PUBLICATION_MIGRATION_STAGE="+stage)
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 86 {
				t.Fatalf("child %v %s", err, out)
			}
			finishPublicationMigration(t, q)
			verifyMigratedNativePublication(t, q, ref, r)
			verifyBackup()
		})
	}
}

func TestWindowsNativePublicationMigrationBusyAndPermanentLock(t *testing.T) {
	q, ref, r, verifyBackup := legacyNativePublication(t, 1)
	root, err := q.root.OpenRoot(publicationDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	holder := &NativePublication{root: root}
	lock, err := holder.lock()
	if err != nil {
		t.Fatal(err)
	}
	before, err := lock.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.MigrateNativePublication(t.Context()); !errors.Is(err, ErrBusy) {
		t.Fatal("migration crossed original lock", err)
	}
	unlockClose(lock)
	finishPublicationMigration(t, q)
	lock, err = holder.lock()
	if err != nil {
		t.Fatal(err)
	}
	after, err := lock.Stat()
	unlockClose(lock)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("lock replaced", err)
	}
	verifyMigratedNativePublication(t, q, ref, r)
	verifyBackup()
}
