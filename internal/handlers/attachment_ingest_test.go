package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/media"
)

// A bot-emitted workspace-file attachment that cannot be persisted must
// surface a catalogued error code, not an unnamed internal failure.
func TestWSAttachmentIngestFailureCarriesCode(t *testing.T) {
	service := media.NewService(slog.Default(), nil)
	service.SetRequirePersistence(true)
	h := &LocalChannelHandler{mediaService: service, logger: slog.Default()}
	event, err := json.Marshal(map[string]any{
		"type": "attachment_delta",
		"attachments": []any{map[string]any{
			"type": "file",
			"path": "/data/downloads/missing.bin",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	processed := h.processWSEvent(context.Background(), "bot", event)
	if len(processed) != 1 {
		t.Fatalf("expected one error frame, got %d", len(processed))
	}
	var frame struct {
		Type string `json:"type"`
		Code string `json:"code"`
	}
	if err := json.Unmarshal(processed[0], &frame); err != nil {
		t.Fatalf("frame decode: %v %s", err, processed[0])
	}
	if frame.Type != "error" || !strings.HasPrefix(frame.Code, "media.") {
		t.Fatalf("attachment ingest failure must carry a media code, got type=%q code=%q", frame.Type, frame.Code)
	}
	if frame.Code != "media.ingest_failed" {
		t.Fatalf("unexpected code: %q", frame.Code)
	}
}
