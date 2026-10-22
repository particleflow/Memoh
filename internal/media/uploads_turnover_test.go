package media

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestTaskReceiptTTLDoesNotEvictUnconfirmedOutcome(t *testing.T) {
	for _, state := range []string{"uploading", "ready", "canceled"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			s := uploadService(t, root, 1024).Uploads()
			s.now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
			body := []byte("unconfirmed")
			r := createTransferUpload(t.Context(), t, s, body)
			switch state {
			case "ready":
				appendBytes(t.Context(), t, s, r, body)
				var err error
				r, err = s.Complete(t.Context(), "owner", "bot", r.ID)
				if err != nil {
					t.Fatal(err)
				}
			case "canceled":
				var err error
				r, err = s.Cancel(t.Context(), "owner", "bot", r.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			s.now = func() time.Time { return r.ExpiresAt.Add(time.Hour) }
			if err := s.Sweep(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(root, ".uploads", r.ID+".json")); err != nil {
				t.Fatal("unconfirmed task evidence evicted by TTL", err)
			}
			got, err := s.Get(t.Context(), "owner", "bot", r.ID)
			if err != nil || got != r {
				t.Fatal("original receipt cannot reconcile", got, err)
			}
			if _, err := s.Get(t.Context(), "other", "bot", r.ID); !errors.Is(err, ErrUploadNotFound) {
				t.Fatal("retention changed owner authorization", err)
			}
		})
	}
}

func TestTaskReceiptTurnoverAcrossRestartsIsBounded(t *testing.T) {
	root := t.TempDir()
	service := uploadService(t, root, 1024)
	s := service.Uploads()
	unknown := createTransferUpload(t.Context(), t, s, []byte("retained unknown"))
	for i := range 3*maxUploadSessions + 7 {
		body := []byte(fmt.Sprintf("synthetic completed file %04d", i))
		r := createTransferUpload(t.Context(), t, s, body)
		appendBytes(t.Context(), t, s, r, body)
		proof, err := s.Complete(t.Context(), "owner", "bot", r.ID)
		if err != nil {
			t.Fatal(err)
		}
		// The storage port receives a trusted committed proof; the service/HTTP
		// tests below separately enforce that ready alone cannot reach this port.
		if err := s.RetireTaskReceipt(t.Context(), "owner", "bot", proof); err != nil {
			t.Fatal(i, err)
		}
		if i%64 == 0 {
			service = uploadService(t, root, 1024)
			s = service.Uploads()
		}
		if err := s.RetireTaskReceipt(t.Context(), "owner", "bot", proof); err != nil {
			t.Fatal("retirement replay", err)
		}
		if _, err := s.Complete(t.Context(), "owner", "bot", r.ID); !errors.Is(err, ErrUploadNotFound) {
			t.Fatal("old writer resurrected receipt", err)
		}
		if _, err := s.Append(t.Context(), "owner", "bot", r.ID, 0, int64(len(body)), digest(body), bytes.NewReader(body)); !errors.Is(err, ErrUploadNotFound) {
			t.Fatal("old retry recreated receipt", err)
		}
		entries, err := s.entries()
		if err != nil || len(entries) > 67 {
			t.Fatal("staging metadata grew with completed history", len(entries), err)
		}
		var metadataBytes int64
		for _, entry := range entries {
			stat, err := entry.Info()
			if err != nil {
				t.Fatal(err)
			}
			metadataBytes += stat.Size()
			if strings.HasSuffix(entry.Name(), ".json") && entry.Name() != unknown.ID+".json" {
				t.Fatal("completed receipt retained")
			}
		}
		if metadataBytes > 4096 {
			t.Fatal("temporary metadata exceeds bound", metadataBytes)
		}
		reader, _, err := service.Open(t.Context(), "bot", proof.ContentHash)
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil || !bytes.Equal(got, body) {
			t.Fatal("original changed", err)
		}
	}
	if _, err := s.Get(t.Context(), "owner", "bot", unknown.ID); err != nil {
		t.Fatal("unknown receipt removed", err)
	}
}

func TestTaskReceiptRetirementRejectsBusyWriter(t *testing.T) {
	root := t.TempDir()
	writer, collector := uploadService(t, root, 1024).Uploads(), uploadService(t, root, 1024).Uploads()
	body := []byte("in-flight")
	r := createTransferUpload(t.Context(), t, writer, body)
	reader := &leasePausedReader{reader: bytes.NewReader(body), entered: make(chan struct{}), resume: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := writer.Append(t.Context(), "owner", "bot", r.ID, 0, int64(len(body)), digest(body), reader)
		done <- err
	}()
	select {
	case <-reader.entered:
	case err := <-done:
		t.Fatal("append did not enter reader", err)
	case <-time.After(5 * time.Second):
		close(reader.resume)
		t.Fatal("append did not acquire its lock")
	}
	proof := r
	proof.State, proof.Offset, proof.ContentHash = "ready", r.SizeBytes, digest(body)
	err := collector.RetireTaskReceipt(t.Context(), "owner", "bot", proof)
	close(reader.resume)
	if !errors.Is(err, ErrUploadBusy) {
		t.Fatal("retired during in-flight write", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := collector.RetireTaskReceipt(t.Context(), "owner", "bot", proof); !errors.Is(err, ErrUploadConflict) {
		t.Fatal("uncommitted bytes looked ready", err)
	}
	proof, err = writer.Complete(t.Context(), "owner", "bot", r.ID)
	if err != nil {
		t.Fatal(err)
	}
	locks := map[string]os.FileInfo{}
	entries, _ := collector.entries()
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "lock-") {
			locks[e.Name()], _ = e.Info()
		}
	}
	if err := collector.RetireTaskReceipt(t.Context(), "owner", "bot", proof); err != nil {
		t.Fatal(err)
	}
	for name, before := range locks {
		after, err := collector.root.Stat(name)
		if err != nil || !os.SameFile(before, after) {
			t.Fatal("lock identity changed", err)
		}
	}
	if _, err := writer.Get(t.Context(), "owner", "bot", r.ID); !errors.Is(err, ErrUploadNotFound) {
		t.Fatal("old store resurrected receipt", err)
	}
}

func TestTaskRetirementFaultsNeverAcknowledgeOrEraseUnknown(t *testing.T) {
	for _, mode := range []string{"owner", "hash", "corrupt", "no-space", "lost-directory-sync", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			s := uploadService(t, root, 1024).Uploads()
			data := []byte("fault")
			r := createTransferUpload(t.Context(), t, s, data)
			appendBytes(t.Context(), t, s, r, data)
			proof, err := s.Complete(t.Context(), "owner", "bot", r.ID)
			if err != nil {
				t.Fatal(err)
			}
			owner := "owner"
			switch mode {
			case "owner":
				owner = "other"
			case "hash":
				proof.ContentHash = digest([]byte("other"))
			case "corrupt":
				if err := os.WriteFile(filepath.Join(root, ".uploads", r.ID+".json"), []byte("broken"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				record, err := s.read(r.ID)
				if err != nil {
					t.Fatal(err)
				}
				record.State = "canceled"
				if err := s.save(record); err != nil {
					t.Fatal(err)
				}
			case "no-space", "lost-directory-sync":
				calls := 0
				s.syncFile = func(f *os.File) error {
					stat, err := f.Stat()
					if err != nil {
						return err
					}
					if stat.IsDir() {
						calls++
					}
					if mode == "no-space" || calls == 2 {
						return syscall.ENOSPC
					}
					return f.Sync()
				}
			}
			if err := s.RetireTaskReceipt(t.Context(), owner, "bot", proof); err == nil {
				t.Fatal("failed retirement acknowledged")
			}
			if mode == "lost-directory-sync" {
				s = uploadService(t, root, 1024).Uploads()
				if err := s.RetireTaskReceipt(t.Context(), "owner", "bot", proof); err != nil {
					t.Fatal("lost sync did not reconcile", err)
				}
			} else if _, err := os.Stat(filepath.Join(root, ".uploads", r.ID+".json")); err != nil {
				t.Fatal("refused retirement erased evidence", err)
			}
		})
	}
}

func TestTaskUnknownReceiptsBackpressureWithoutTTLEviction(t *testing.T) {
	root := t.TempDir()
	s := uploadService(t, root, 1024).Uploads()
	for range maxUploadSessions {
		r := createTransferUpload(t.Context(), t, s, []byte("unknown ready"))
		appendBytes(t.Context(), t, s, r, []byte("unknown ready"))
		if _, err := s.Complete(t.Context(), "owner", "bot", r.ID); err != nil {
			t.Fatal(err)
		}
	}
	s.now = func() time.Time { return time.Now().Add(10 * UploadTTL) }
	_, err := s.CreateTransfer(t.Context(), "owner", "bot", UploadInput{1, digest([]byte("x")), "application/octet-stream"})
	if !errors.Is(err, ErrUploadQuota) {
		t.Fatal("pressure evicted unknown receipts", err)
	}
	entries, err := s.entries()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			count++
		}
	}
	if count != maxUploadSessions {
		t.Fatal("lost unknown records", count)
	}
}

func TestPriorTaskLeaseUpgradesWithoutExpiryEviction(t *testing.T) {
	for _, trigger := range []string{"startup", "periodic", "create"} {
		t.Run(trigger, func(t *testing.T) {
			root := t.TempDir()
			s := uploadService(t, root, 1024).Uploads()
			body := []byte("original-task-evidence")
			r := createTransferUpload(t.Context(), t, s, body)
			r = appendBytes(t.Context(), t, s, r, body[:1])
			record, err := s.read(r.ID)
			if err != nil {
				t.Fatal(err)
			}
			record.ProgressLease = 1
			if err := s.save(record); err != nil {
				t.Fatal(err)
			}
			if trigger == "startup" {
				s = uploadService(t, root, 1024).Uploads()
			}
			s.now = func() time.Time { return r.ExpiresAt.Add(time.Hour) }
			if trigger == "create" {
				createTransferUpload(t.Context(), t, s, []byte("new"))
			} else if err := s.Sweep(t.Context()); err != nil {
				t.Fatal(err)
			}
			got, err := s.read(r.ID)
			if err != nil || got.ProgressLease != 2 || got.Offset != r.Offset || !got.ExpiresAt.Equal(r.ExpiresAt) {
				t.Fatal("lease migration lost or renewed evidence", err)
			}
			payload, err := os.ReadFile(filepath.Join(root, ".uploads", r.ID+".data")) //nolint:gosec // Exact synthetic receipt in this isolated root.
			if err != nil || !bytes.Equal(payload, body[:1]) {
				t.Fatal("payload changed", err)
			}
		})
	}
}
