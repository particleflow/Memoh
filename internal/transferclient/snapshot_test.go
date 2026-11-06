//go:build linux || darwin

package transferclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"testing"
	"time"
)

func snapshotFixture(t *testing.T, size int64) (*DiskQueue, Job) {
	t.Helper()
	limits := queueLimits()
	limits.Bytes = 2*size + 32<<20
	q := openQueue(t, t.TempDir(), limits)
	f, err := os.CreateTemp(t.TempDir(), "producer")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.CopyBuffer(io.MultiWriter(f, h), io.LimitReader(zeroReader{}, size), make([]byte, copyBytes)); err != nil {
		t.Fatal(err)
	}
	j := newJob([]byte("x"))
	j.SnapshotID = j.DeliveryID
	j.Spec.SizeBytes = size
	j.Spec.SHA256 = hex.EncodeToString(h.Sum(nil))
	if err := q.Admit(t.Context(), "upload", j, f); err != nil {
		t.Fatal(err)
	}
	return q, j
}

type countingReadAt struct {
	io.ReaderAt
	bytes int64
}

func (r *countingReadAt) ReadAt(p []byte, offset int64) (int, error) {
	n, err := r.ReaderAt.ReadAt(p, offset)
	r.bytes += int64(n)
	return n, err
}

func TestSealedSnapshotLinearReadsAcrossReopen(t *testing.T) {
	const size = int64(64 << 20)
	q, j := snapshotFixture(t, size)
	r := inspectQueue(t, q, j.DeliveryID)
	buffer := make([]byte, MaxChunkBytes)
	dataBytes, proofBytes := int64(0), int64(0)
	for offset := int64(0); offset < size; offset += int64(len(buffer)) {
		source, closeSource, err := q.openSnapshot(r)
		if err != nil {
			t.Fatal(err)
		}
		s := source.(sealedSnapshot)
		data, proof := &countingReadAt{ReaderAt: s.data}, &countingReadAt{ReaderAt: s.tree}
		s.data, s.tree = data, proof
		if err := s.ReadAt(t.Context(), j.SnapshotID, j.LocatorRevision, j.Spec, buffer, offset); err != nil {
			closeSource()
			t.Fatal(err)
		}
		closeSource()
		dataBytes += data.bytes
		proofBytes += proof.bytes
	}
	if dataBytes != size || proofBytes > 64*6*sha256.Size {
		t.Fatal("unbounded full-snapshot rereads", dataBytes, proofBytes)
	}
	t.Logf("64 MiB, 8 reopened chunk handles: data=%d proof=%d bytes; plus one 32-byte root check per open", dataBytes, proofBytes)
}

func TestSnapshotSchemaRejectsLegacyWithoutFallback(t *testing.T) {
	path := t.TempDir()
	q := openQueue(t, path, queueLimits())
	f, err := q.file("locator", os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	var header map[string]any
	if json.Unmarshal(body, &header) != nil {
		t.Fatal("fixture header")
	}
	header["Version"] = 1
	body, err = json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	if f.Truncate(0) != nil {
		t.Fatal("fixture truncate")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(body); err != nil {
		t.Fatal(err)
	}
	_ = f.Sync()
	_ = f.Close()
	_ = q.Close()
	other, err := OpenHerdrQueue(func(string) string { return path }, "revision-1", queueLimits())
	if other != nil {
		_ = other.Close()
	}
	if !errors.Is(err, ErrVersion) {
		t.Fatal("legacy schema silently reused", err)
	}
}

func TestSealedSnapshotOddGeometryAndUnalignedReads(t *testing.T) {
	for _, size := range []int64{1, snapshotBlockBytes + 17, 3*snapshotBlockBytes + 17} {
		t.Run(strconv.FormatInt(size, 10), func(t *testing.T) {
			q, j := snapshotFixture(t, size)
			r := inspectQueue(t, q, j.DeliveryID)
			source, closeSource, err := q.openSnapshot(r)
			if err != nil {
				t.Fatal(err)
			}
			defer closeSource()
			for _, offset := range []int64{0, size / 2, size - 1} {
				buffer := make([]byte, min(size-offset, 2*snapshotBlockBytes))
				if err := source.ReadAt(t.Context(), j.SnapshotID, j.LocatorRevision, j.Spec, buffer, offset); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if err := source.ReadAt(ctx, j.SnapshotID, j.LocatorRevision, j.Spec, make([]byte, 1), 0); !errors.Is(err, context.Canceled) {
				t.Fatal("canceled proof ignored", err)
			}
		})
	}
}

func TestSealedSnapshotTamperAndIdentity(t *testing.T) {
	for _, kind := range []string{"data", "proof", "root", "identity"} {
		t.Run(kind, func(t *testing.T) {
			q, j := snapshotFixture(t, 3*snapshotBlockBytes+17)
			r := inspectQueue(t, q, j.DeliveryID)
			name := j.DeliveryID + ".snapshot"
			offset := int64(0)
			if kind == "proof" || kind == "root" {
				name = j.DeliveryID + ".tree"
				offset = sha256.Size
			}
			if kind == "root" {
				offset = snapshotTreeBytes(j.Spec.SizeBytes) - 1
			}
			if kind == "identity" {
				r.Seal.DataIdentity = "different-file-identity"
			} else {
				if err := q.root.Chmod(name, 0o600); err != nil {
					t.Fatal(err)
				}
				f, err := q.file(name, os.O_RDWR, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				var one [1]byte
				if _, err := f.ReadAt(one[:], offset); err != nil {
					t.Fatal(err)
				}
				one[0] ^= 0xff
				if _, err := f.WriteAt(one[:], offset); err != nil {
					t.Fatal(err)
				}
				if f.Sync() != nil || f.Chmod(0o400) != nil {
					t.Fatal("fixture tamper failed")
				}
				_ = f.Close()
			}
			source, closeSource, err := q.openSnapshot(r)
			if kind == "root" || kind == "identity" {
				if !errors.Is(err, ErrSnapshot) {
					if closeSource != nil {
						closeSource()
					}
					t.Fatal("bad root/identity accepted", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer closeSource()
			if err := source.ReadAt(t.Context(), j.SnapshotID, j.LocatorRevision, j.Spec, make([]byte, 17), 0); !errors.Is(err, ErrSnapshot) {
				t.Fatal("tampered block/proof accepted", err)
			}
		})
	}
}

type canceledSnapshot struct{}

func (canceledSnapshot) ReadAt(context.Context, string, string, Spec, []byte, int64) error {
	return context.DeadlineExceeded
}

func TestSnapshotDeadlinePreservesUploadResume(t *testing.T) {
	_, client := newFixture(t)
	job := newJob([]byte("example"))
	save := func(next Job) error { job = next; return nil }
	if err := client.Advance(t.Context(), job, save, frozen("example"), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := client.Advance(t.Context(), job, save, canceledSnapshot{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if job.Phase != "uploading" || job.Attempts != 1 || job.NextAttempt.IsZero() {
		t.Fatal("snapshot deadline permanently blocked upload")
	}
}

// Timed work covers integrity verification of a complete post-admission file,
// reopening local files for each 8 MiB action and reusing the caller buffer.
// It excludes HTTP, client chunk allocation and production ACL/identity checks.
func BenchmarkSealedSnapshotTransfer(b *testing.B) {
	for _, size := range []int64{1 << 20, 64 << 20, 512 << 20} {
		b.Run(strconv.FormatInt(size>>20, 10)+"MiB", func(b *testing.B) {
			dir := b.TempDir()
			data, err := os.CreateTemp(dir, "data")
			if err != nil {
				b.Fatal(err)
			}
			defer func() { _ = data.Close() }()
			tree, err := os.CreateTemp(dir, "tree")
			if err != nil {
				b.Fatal(err)
			}
			defer func() { _ = tree.Close() }()
			h := sha256.New()
			_, _ = io.CopyBuffer(h, io.LimitReader(zeroReader{}, size), make([]byte, copyBytes))
			job := newJob([]byte("x"))
			job.Spec.SizeBytes = size
			job.Spec.SHA256 = hex.EncodeToString(h.Sum(nil))
			seal, err := sealSnapshot(b.Context(), io.LimitReader(zeroReader{}, size), data, tree, job.Spec)
			if err != nil {
				b.Fatal(err)
			}
			buffer := make([]byte, MaxChunkBytes)
			b.SetBytes(size)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				for offset := int64(0); offset < size; offset += MaxChunkBytes {
					// Reopen directly inside this test-owned directory; production
					// openSnapshot additionally enforces IDs, permissions and locks.
					d, err := os.Open(data.Name()) //nolint:gosec // Name belongs to the file created by this benchmark under TempDir.
					if err != nil {
						b.Fatal(err)
					}
					p, err := os.Open(tree.Name()) //nolint:gosec // Name belongs to the file created by this benchmark under TempDir.
					if err != nil {
						_ = d.Close()
						b.Fatal(err)
					}
					s := sealedSnapshot{data: d, tree: p, job: job, seal: *seal}
					err = s.ReadAt(b.Context(), job.SnapshotID, job.LocatorRevision, job.Spec, buffer[:min(int64(len(buffer)), size-offset)], offset)
					_ = d.Close()
					_ = p.Close()
					if err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

func BenchmarkSnapshotAdmission(b *testing.B) {
	for _, size := range []int64{1 << 20, 64 << 20, 512 << 20} {
		b.Run(strconv.FormatInt(size>>20, 10)+"MiB", func(b *testing.B) {
			dir := b.TempDir()
			h := sha256.New()
			_, _ = io.CopyBuffer(h, io.LimitReader(zeroReader{}, size), make([]byte, copyBytes))
			spec := Spec{SizeBytes: size, SHA256: hex.EncodeToString(h.Sum(nil)), Mime: "application/octet-stream"}
			b.SetBytes(size)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				data, err := os.CreateTemp(dir, "data")
				if err != nil {
					b.Fatal(err)
				}
				tree, err := os.CreateTemp(dir, "tree")
				if err != nil {
					_ = data.Close()
					b.Fatal(err)
				}
				_, err = sealSnapshot(b.Context(), io.LimitReader(zeroReader{}, size), data, tree, spec)
				_ = data.Close()
				_ = tree.Close()
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
