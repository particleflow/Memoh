package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTransferUploadProgressSurvivesOriginalExpiry(t *testing.T) {
	for _, sweep := range []string{"startup", "periodic", "create"} {
		t.Run(sweep, func(t *testing.T) {
			ctx := t.Context()
			root := t.TempDir()
			s := uploadService(t, root, 1024).Uploads()
			now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			s.now = func() time.Time { return now }
			body := []byte("lease")
			u := createTransferUpload(ctx, t, s, body)
			createdDeadline := u.ExpiresAt
			now = now.Add(23 * time.Hour)
			u = appendBytes(ctx, t, s, u, body[:1])
			now = createdDeadline.Add(time.Hour)
			// Startup and hourly cleanup use the same public Sweep. Reopening
			// demonstrates that the progress deadline is a durable receipt field.
			if sweep == "startup" {
				s = uploadService(t, root, 1024).Uploads()
				s.now = func() time.Time { return now }
			}
			if sweep == "create" {
				createUpload(ctx, t, s, []byte("independent"))
			} else if err := s.Sweep(ctx); err != nil {
				t.Fatal(err)
			}
			resumed, err := s.Get(ctx, "owner", "bot", u.ID)
			if err != nil || resumed.ID != u.ID || resumed.Offset != 1 || !resumed.ExpiresAt.Equal(createdDeadline.Add(23*time.Hour)) {
				t.Fatalf("forward progress lost across original deadline: %+v %v", resumed, err)
			}
			resumed = appendBytes(ctx, t, s, resumed, body[1:])
			now = now.Add(time.Hour)
			ready, err := s.Complete(ctx, "owner", "bot", resumed.ID)
			if err != nil || ready.ContentHash != digest(body) || !ready.ExpiresAt.Equal(now.Add(UploadTTL)) {
				t.Fatal(ready, err)
			}
			now = now.Add(time.Hour)
			again, err := s.Complete(ctx, "owner", "bot", resumed.ID)
			if err != nil || again != ready {
				t.Fatal("ready replay renewed or changed receipt", again, err)
			}
			now = ready.ExpiresAt
			if err := s.Sweep(ctx); err != nil {
				t.Fatal(err)
			}
			if got, err := s.Get(ctx, "owner", "bot", u.ID); err != nil || got != ready {
				t.Fatal("uncommitted task ready evidence expired", got, err)
			}
		})
	}
}

func TestTransferUploadLeaseRequiresDurableForwardProgress(t *testing.T) {
	for _, failure := range []string{"get", "wrong-owner", "wrong-offset", "wrong-hash", "canceled-request", "incomplete-complete", "payload-sync", "journal-sync", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			ctx := t.Context()
			root := t.TempDir()
			s := uploadService(t, root, 1024).Uploads()
			now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			s.now = func() time.Time { return now }
			body := []byte("lease")
			u := createTransferUpload(ctx, t, s, body)
			u = appendBytes(ctx, t, s, u, body[:1])
			now = now.Add(23 * time.Hour)
			var err error
			switch failure {
			case "get":
				_, err = s.Get(ctx, "owner", "bot", u.ID)
			case "wrong-owner":
				_, err = s.Append(ctx, "other", "bot", u.ID, 1, 1, digest(body[1:2]), bytes.NewReader(body[1:2]))
			case "wrong-offset":
				_, err = s.Append(ctx, "owner", "bot", u.ID, 0, 1, digest(body[:1]), bytes.NewReader(body[:1]))
			case "wrong-hash":
				_, err = s.Append(ctx, "owner", "bot", u.ID, 1, 1, digest([]byte("wrong")), bytes.NewReader(body[1:2]))
			case "canceled-request":
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				_, err = s.Append(canceled, "owner", "bot", u.ID, 1, 1, digest(body[1:2]), bytes.NewReader(body[1:2]))
			case "incomplete-complete":
				_, err = s.Complete(ctx, "owner", "bot", u.ID)
			case "payload-sync", "journal-sync":
				suffix := ".data"
				if failure == "journal-sync" {
					suffix = ".tmp"
				}
				s.syncFile = func(f *os.File) error {
					if strings.HasSuffix(f.Name(), suffix) {
						return errors.New("synthetic sync failure")
					}
					return f.Sync()
				}
				_, err = s.Append(ctx, "owner", "bot", u.ID, 1, 1, digest(body[1:2]), bytes.NewReader(body[1:2]))
			case "cancel":
				_, err = s.Cancel(ctx, "owner", "bot", u.ID)
			}
			if (failure == "get" || failure == "cancel") != (err == nil) {
				t.Fatalf("unexpected operation outcome: %v", err)
			}
			s = uploadService(t, root, 1024).Uploads()
			s.now = func() time.Time { return now }
			got, err := s.Get(ctx, "owner", "bot", u.ID)
			if err != nil || got.Offset != u.Offset || !got.ExpiresAt.Equal(u.ExpiresAt) {
				t.Fatal("no durable forward progress, but offset/lease changed", got, err)
			}
			now = u.ExpiresAt
			if after, err := s.Get(ctx, "owner", "bot", u.ID); err != nil || after != got {
				t.Fatal("idle unknown receipt changed", after, err)
			}
			if err := s.Sweep(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(root, ".uploads", u.ID+".json")); err != nil {
				t.Fatal("idle unknown evidence collected", err)
			}
			if failure != "cancel" {
				if _, err := os.Stat(filepath.Join(root, ".uploads", u.ID+".data")); err != nil {
					t.Fatal("unknown payload collected", err)
				}
			}
		})
	}
}

func TestTransferUploadLeaseRecoversUnknownAcknowledgement(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	s := uploadService(t, root, 1024).Uploads()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	body := []byte("lease")
	u := createTransferUpload(ctx, t, s, body)
	now = now.Add(23 * time.Hour)
	failure := failJournalDirectorySync(s)
	if _, err := s.Append(ctx, "owner", "bot", u.ID, 0, 1, digest(body[:1]), bytes.NewReader(body[:1])); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "owner", "bot", u.ID); !errors.Is(err, failure) {
		t.Fatal("acknowledged before directory barrier recovery", err)
	}
	s = uploadService(t, root, 1024).Uploads()
	s.now = func() time.Time { return now }
	got, err := s.Get(ctx, "owner", "bot", u.ID)
	if err != nil || got.Offset != 1 || !got.ExpiresAt.Equal(now.Add(UploadTTL)) {
		t.Fatal("offset and lease did not recover together", got, err)
	}
	now = now.Add(-time.Hour)
	got = appendBytes(ctx, t, s, got, body[1:2])
	if !got.ExpiresAt.Equal(u.ExpiresAt.Add(23 * time.Hour)) {
		t.Fatal("backwards clock shortened lease", got)
	}
	now = u.ExpiresAt.Add(time.Hour)
	if err := s.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "owner", "bot", u.ID); err != nil {
		t.Fatal("recovered progress collected at original deadline", err)
	}
}

func TestLegacyUploadProgressKeepsFixedExpiry(t *testing.T) {
	ctx := t.Context()
	s := uploadService(t, t.TempDir(), 1024).Uploads()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	body := []byte("legacy")
	u := createUpload(ctx, t, s, body)
	now = now.Add(23 * time.Hour)
	got := appendBytes(ctx, t, s, u, body)
	now = now.Add(30 * time.Minute)
	ready, err := s.Complete(ctx, "owner", "bot", u.ID)
	if err != nil || !got.ExpiresAt.Equal(u.ExpiresAt) || !ready.ExpiresAt.Equal(u.ExpiresAt) {
		t.Fatal("changed generic fixed-expiry contract", got, ready, err)
	}
}

func TestTransferUploadUnknownLeasePolicyFailsClosed(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	s := uploadService(t, root, 1024).Uploads()
	u := createTransferUpload(ctx, t, s, []byte("policy"))
	r, err := s.read(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.ProgressLease = 3
	if err := s.save(r); err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return u.ExpiresAt.Add(time.Hour) }
	if _, err := s.Get(ctx, "owner", "bot", u.ID); !errors.Is(err, ErrUploadInvalid) {
		t.Fatal(err)
	}
	if err := s.Sweep(ctx); !errors.Is(err, ErrUploadInvalid) {
		t.Fatal("unknown policy was collected", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".uploads", u.ID+".json")); err != nil {
		t.Fatal("unknown evidence removed", err)
	}
}

func createTransferUpload(ctx context.Context, t *testing.T, s *UploadStore, b []byte) Upload {
	t.Helper()
	u, err := s.CreateTransfer(ctx, "owner", "bot", UploadInput{int64(len(b)), digest(b), "application/octet-stream"})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestReceiptRetentionPreservesOriginal(t *testing.T) {
	for _, policy := range []struct {
		name   string
		create func(context.Context, *testing.T, *UploadStore, []byte) Upload
	}{{"legacy", createUpload}, {"task-lease", createTransferUpload}} {
		for _, trigger := range []string{"startup", "periodic", "create"} {
			t.Run(policy.name+"/"+trigger, func(t *testing.T) {
				ctx := t.Context()
				root := t.TempDir()
				svc := uploadService(t, root, 1024)
				s := svc.Uploads()
				now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
				s.now = func() time.Time { return now }
				body := []byte("synthetic retained original")
				u := policy.create(ctx, t, s, body)
				now = now.Add(23 * time.Hour)
				appendBytes(ctx, t, s, u, body)
				now = now.Add(30 * time.Minute)
				ready, err := s.Complete(ctx, "owner", "bot", u.ID)
				if err != nil {
					t.Fatal(err)
				}
				now = ready.ExpiresAt
				if trigger == "startup" {
					s = uploadService(t, root, 1024).Uploads()
					s.now = func() time.Time { return now }
				}
				if trigger == "create" {
					createUpload(ctx, t, s, []byte("other"))
				} else if err := s.Sweep(ctx); err != nil {
					t.Fatal(err)
				}
				if policy.name == "legacy" {
					if _, err := s.Get(ctx, "owner", "bot", u.ID); !errors.Is(err, ErrUploadNotFound) {
						t.Fatal("legacy TTL changed", err)
					}
					if _, err := os.Stat(filepath.Join(root, ".uploads", u.ID+".json")); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("legacy GC changed", err)
					}
				} else if got, err := s.Get(ctx, "owner", "bot", u.ID); err != nil || got != ready {
					t.Fatal("uncommitted ready evidence collected", got, err)
				}
				reader, _, err := svc.Open(ctx, "bot", ready.ContentHash)
				if err != nil {
					t.Fatal(err)
				}
				got, err := io.ReadAll(reader)
				_ = reader.Close()
				if err != nil || !bytes.Equal(got, body) || digest(got) != ready.ContentHash {
					t.Fatal("receipt collection changed the original", err)
				}
			})
		}
	}
}
