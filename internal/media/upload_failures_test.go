package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUploadFailureQueueDurableScopedAndIndependent(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	svc := uploadService(t, root, 1024)
	s := svc.Uploads()
	const originalBytes = "committed original survives failure journal expiry"
	asset, err := svc.Ingest(ctx, IngestInput{BotID: "bot", Mime: "text/plain", Reader: strings.NewReader(originalBytes)})
	if err != nil {
		t.Fatal(err)
	}
	original := createUpload(ctx, t, s, []byte("unchanged queued original"))
	before, err := os.ReadFile(filepath.Join(root, ".uploads", original.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	operation := strings.Repeat("a", 32)
	input := UploadFailureInput{Version: 1, Phase: "source-open", Code: "VOICE_UNAVAILABLE", SDKCode: 13900012, At: time.Now().UnixMilli()}
	first, err := s.ReportFailure(ctx, "owner", "bot", operation, input)
	if err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(filepath.Join(root, ".uploads", failureJournal))
	if err != nil {
		t.Fatal(err)
	}
	restarted := uploadService(t, root, 1024).Uploads()
	same, err := restarted.ReportFailure(ctx, "owner", "bot", operation, input)
	if err != nil || same != first {
		t.Fatal(same, err)
	}
	got, err := restarted.Failures(ctx, "owner", "bot", operation)
	if err != nil || len(got) != 1 || got[0] != first {
		t.Fatal(got, err)
	}
	input.Phase = "snapshot-copy"
	if _, err := restarted.ReportFailure(ctx, "owner", "bot", operation, input); !errors.Is(err, ErrUploadConflict) {
		t.Fatal(err)
	}
	for _, scope := range [][2]string{{"other", "bot"}, {"owner", "other"}} {
		if _, err := restarted.Failures(ctx, scope[0], scope[1], operation); !errors.Is(err, ErrUploadNotFound) {
			t.Fatal(err)
		}
	}
	after, _ := os.ReadFile(filepath.Join(root, ".uploads", original.ID+".json"))
	if !bytes.Equal(before, after) {
		t.Fatal("upload receipt changed")
	}
	after, _ = os.ReadFile(filepath.Join(root, ".uploads", failureJournal))
	if !bytes.Equal(disk, after) {
		t.Fatal("read/repeat/conflict modified first report")
	}
	if _, err := s.ReportFailure(ctx, "owner", "bot", strings.Repeat("b", 32), UploadFailureInput{Version: 1, Phase: "SECRET_URI", Code: "VOICE_UNAVAILABLE", At: 1}); !errors.Is(err, ErrUploadInvalid) {
		t.Fatal(err)
	}
	s.now = func() time.Time { return first.ExpiresAt.Add(time.Second) }
	if _, err := s.Failures(ctx, "owner", "bot", operation); !errors.Is(err, ErrUploadNotFound) {
		t.Fatal(err)
	}
	if err := s.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".uploads", failureJournal)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	reader, _, err := svc.Open(ctx, "bot", asset.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != originalBytes || digest(data) != asset.ContentHash {
		t.Fatal("failure GC changed committed original", err)
	}
}

func TestUploadFailureUnknownAckAndSymlinkBoundary(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := uploadService(t, root, 1024).Uploads()
	input := UploadFailureInput{Version: 1, Phase: "journal-dir-sync", Code: "UNCLASSIFIED", SDKCode: 13900020, At: 1}
	operation := strings.Repeat("a", 32)
	s.syncFile = func(f *os.File) error {
		info, err := f.Stat()
		if err != nil {
			return err
		}
		if info.IsDir() {
			return errors.New("synthetic directory sync failure")
		}
		return f.Sync()
	}
	if _, err := s.ReportFailure(ctx, "owner", "bot", operation, input); err == nil {
		t.Fatal("expected unacknowledged directory barrier")
	}
	restarted := uploadService(t, root, 1024).Uploads()
	records, err := restarted.Failures(ctx, "owner", "bot", operation)
	if err != nil || len(records) != 1 {
		t.Fatal(records, err)
	}
	first := records[0]
	same, err := restarted.ReportFailure(ctx, "owner", "bot", operation, input)
	if err != nil || same != first {
		t.Fatal(same, err)
	}
	external := filepath.Join(t.TempDir(), "external")
	if err := os.WriteFile(external, []byte("unchanged external"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, ".uploads", failureJournal)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(root, ".uploads", failureJournal)); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.ReportFailure(ctx, "owner", "bot", operation, input); err == nil {
		t.Fatal("followed linked journal")
	}
	data, _ := os.ReadFile(external)
	if string(data) != "unchanged external" {
		t.Fatal("external changed")
	}
}

func TestUploadFailureWindowDoesNotConsumeUploadSlots(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := uploadService(t, root, 1024).Uploads()
	now := s.now().UTC()
	in := UploadFailureInput{Version: 1, Phase: "source-open", Code: "VOICE_UNAVAILABLE", At: now.UnixMilli()}
	records := make([]uploadFailureRecord, maxUploadSessions)
	for i := range records {
		records[i] = uploadFailureRecord{UploadFailure: UploadFailure{UploadFailureInput: in, Operation: strings.Repeat("a", 30) + "00", ReceivedAt: now, ExpiresAt: now.Add(UploadTTL)}, Owner: "owner", Bot: "bot"}
	}
	// Arrange the existing bounded metadata window without performing uploads.
	for i := range records {
		records[i].Operation = fmtOperation(i)
	}
	if err := s.saveFailures(records); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReportFailure(ctx, "owner", "bot", strings.Repeat("f", 32), in); !errors.Is(err, ErrUploadQuota) {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, "owner", "bot", UploadInput{SizeBytes: 3, SHA256: digest([]byte("abc")), Mime: "text/plain"}); err != nil {
		t.Fatal("metadata consumed upload admission", err)
	}
	raw, _ := json.Marshal(records)
	if bytes.Contains(raw, []byte("Bearer")) {
		t.Fatal("credential captured")
	}
}

func fmtOperation(i int) string {
	const hex = "0123456789abcdef"
	return strings.Repeat("a", 30) + string([]byte{hex[i/16], hex[i%16]})
}
