//go:build windows

package transferclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func publicationFixtureSource(t *testing.T, q *DiskQueue, data []byte) (Reference, *publicationSource) {
	t.Helper()
	j := newJob(data)
	j.SnapshotID = j.DeliveryID
	if err := q.Admit(t.Context(), "download", j, nil); err != nil {
		t.Fatal(err)
	}
	f, err := q.file(j.DeliveryID+".part", os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil || f.Sync() != nil {
		t.Fatal("source write", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if err := Verify(t.Context(), f, j.Spec); err != nil {
		t.Fatal(err)
	}
	ref := Reference{DeliveryID: j.DeliveryID, OperationID: j.OperationID, BotID: j.BotID, ContentHash: j.Spec.SHA256, SizeBytes: j.Spec.SizeBytes, Mime: j.Spec.Mime}
	source := &publicationSource{SectionReader: io.NewSectionReader(f, 0, j.Spec.SizeBytes), file: f, queue: q, reference: ref, active: true}
	t.Cleanup(func() { source.active = false; _ = f.Close() })
	return ref, source
}

func nativePublication(t *testing.T, q *DiskQueue, ref Reference) *NativePublication {
	t.Helper()
	p, err := q.OpenNativePublication(func(ctx context.Context, candidate Reference) error {
		if candidate != ref {
			return ErrAuthorization
		}
		return ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func TestWindowsNativePublicationRoundTrip(t *testing.T) {
	q := nativeQueue(t, nativeFixtureRoot(t, false))
	data := []byte("native publication reference")
	ref, source := publicationFixtureSource(t, q, data)
	p := nativePublication(t, q, ref)
	before, err := queueFileIdentity(source.file)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := p.Publish(t.Context(), ref, source); err != nil || out != OutcomeCommitted {
		t.Fatal("publish", out, err)
	}
	if _, err := q.root.Lstat(queueName(ref.DeliveryID + ".part")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("source was copied instead of transferred", err)
	}
	after, err := queueFileIdentity(source.file)
	if err != nil || after != before {
		t.Fatal("rename lost source identity", after, err)
	}
	source.active = false
	if err := source.file.Close(); err != nil {
		t.Fatal(err)
	}
	for _, cancel := range []bool{false, true} {
		if out, err := p.Resolve(t.Context(), ref, cancel); err != nil || out != OutcomeCommitted {
			t.Fatal("resolve published", out, err)
		}
	}
	reader, err := p.OpenPublished(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("read publication", err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	p = nativePublication(t, q, ref)
	if out, err := p.Resolve(t.Context(), ref, true); err != nil || out != OutcomeCommitted {
		t.Fatal("reopened published", out, err)
	}
}

func TestWindowsNativePublicationNeverOverwritesTarget(t *testing.T) {
	q := nativeQueue(t, nativeFixtureRoot(t, false))
	ref, source := publicationFixtureSource(t, q, []byte("new data"))
	p := nativePublication(t, q, ref)
	path := filepath.Join(p.root.Name(), publicationKey(ref)+".object")
	if err := os.WriteFile(path, []byte("existing target"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Publish(t.Context(), ref, source); !errors.Is(err, ErrPublicationConflict) {
		t.Fatal("existing target accepted", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "existing target" {
		t.Fatal("existing bytes changed", err)
	}
}

func TestWindowsNativePublicationFenceAndScope(t *testing.T) {
	q := nativeQueue(t, nativeFixtureRoot(t, false))
	ref, source := publicationFixtureSource(t, q, []byte("fenced data"))
	p := nativePublication(t, q, ref)
	if out, err := p.Resolve(t.Context(), ref, true); err != nil || out != OutcomeCanceled {
		t.Fatal("fence", out, err)
	}
	if out, err := p.Publish(t.Context(), ref, source); err != nil || out != OutcomeCanceled {
		t.Fatal("late publish crossed fence", out, err)
	}
	if _, err := p.root.Lstat(publicationKey(ref) + ".object"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fence created bytes", err)
	}
	other := ref
	other.OperationID = "other-operation"
	if _, err := p.Resolve(t.Context(), other, false); !errors.Is(err, ErrAuthorization) {
		t.Fatal("scope bypass", err)
	}
	if _, err := p.Publish(t.Context(), ref, bytes.NewReader([]byte("fenced data"))); !errors.Is(err, ErrBinding) {
		t.Fatal("raw reader forged source proof", err)
	}
}
