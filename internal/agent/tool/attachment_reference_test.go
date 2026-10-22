package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/agent/sessionmode"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

// Exercise the ordinary text result + send(content_hash) path with a real file
// store. The deterministic SpawnAgent is not a live model or an external agent.
func TestSubagentTextReferenceCanUseParentSend(t *testing.T) {
	m := media.NewService(nil, localfs.NewOriginals(t.TempDir()))
	m.SetRequirePersistence(true)
	asset, err := m.Ingest(t.Context(), media.IngestInput{BotID: "bot-1", Mime: "text/plain", Reader: strings.NewReader("synthetic child artifact")})
	if err != nil {
		t.Fatal(err)
	}
	agent := &fakeSpawnAgent{}
	spawn, _, _, _ := newAgentControlProvider(t, agent)
	parent := SessionContext{BotID: "bot-1", SessionID: "parent-1", UserID: "user-1", SessionType: sessionmode.Chat, CurrentPlatform: "telegram", ReplyTarget: "chat-1"}
	result := asMap(t, mustExecuteAgentTool(t, spawn, parent, "spawn_agent", map[string]any{"id": "worker", "task": asset.ContentHash}))
	if result["status"] != "completed" || result["text"] != "report for "+asset.ContentHash || len(agent.queries()) != 1 {
		t.Fatal("ordinary child result was not preserved", result)
	}
	for _, field := range []string{"image_tasks", "image_task_ids", "artifact_refs"} {
		if _, present := result[field]; present {
			t.Fatal("text-only child acquired a specialized result contract", field)
		}
	}
	// The parent explicitly selects the reference from the child's text; there
	// is no automatic promotion of arbitrary child text to an attachment.
	ref := strings.TrimPrefix(result["text"].(string), "report for ")
	sender := &recordingSender{}
	messages := NewMessageProvider(nil, sender, usageTestReactor{}, usageTestResolver{}, m)
	_, err = messages.execSend(t.Context(), parent, "send-ref", map[string]any{"attachments": []any{map[string]any{"content_hash": ref, "type": "file"}}})
	if err != nil || sender.called != 1 || len(sender.req.Message.Attachments) != 1 || sender.req.Message.Attachments[0].ContentHash != asset.ContentHash {
		t.Fatal("parent send did not preserve the reference", err)
	}
	r, _, err := m.Open(t.Context(), parent.BotID, ref)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil || string(data) != "synthetic child artifact" {
		t.Fatal("attachment changed", err)
	}
	// This store has no delegation or artifact binding to another Bot. This
	// negative lookup does not specify whether authorized Bot delegation is
	// allowed, and does not test a supervisor-to-specialist task flow.
	if _, err := m.Stat(t.Context(), "unrelated-bot", ref); err == nil {
		t.Fatal("unrelated Bot without an artifact binding resolved the hash")
	}
}

func TestOriginalImageGenerationUsesSharedMediaReferences(t *testing.T) {
	m := media.NewService(nil, localfs.NewOriginals(t.TempDir()))
	m.SetRequirePersistence(true)
	p := &ImageGenProvider{media: m}
	var events []ToolStreamEvent
	session := liveShortcutSession(func(event ToolStreamEvent) { events = append(events, event) })
	session.BotID = "bot-1"
	image := generatedImage{Data: []byte("synthetic opaque image bytes"), MediaType: "image/png"}
	result, err := p.persistGeneratedImage(t.Context(), session, "generation", image)
	if err != nil {
		t.Fatal(err)
	}
	ref := result.(map[string]any)["content_hash"].(string)
	if len(events) != 1 || events[0].Attachments[0].ContentHash != ref || events[0].Attachments[0].Path != "" || events[0].Attachments[0].Base64 != "" {
		t.Fatal("image did not use the existing attachment reference", events)
	}
	r, _, err := m.Open(t.Context(), session.BotID, ref)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil || !bytes.Equal(got, image.Data) {
		t.Fatal("published content changed", err)
	}
	session.IsSubagent, session.SessionType = true, sessionmode.Subagent
	_, err = p.persistGeneratedImage(t.Context(), session, "child-generation", image)
	if err != nil || len(events) != 1 {
		t.Fatal("child emitted directly to the parent", err)
	}
	p.media = media.NewService(nil, nil)
	result, err = p.persistGeneratedImage(t.Context(), session, "failed-store", image)
	encoded, _ := json.Marshal(result)
	if err != nil || bytes.Contains(encoded, image.Data) || bytes.Contains(encoded, []byte("content_hash")) || bytes.Contains(encoded, []byte("base64")) {
		t.Fatal("storage failure leaked bytes or a ready reference", result, err)
	}
}

func TestOriginalImageToolRequiresStorageBeforeGeneration(t *testing.T) {
	p := &ImageGenProvider{}
	_, err := p.execGenerateImage(context.Background(), SessionContext{BotID: "bot-1"}, "generation", generateImageArgs{Prompt: "synthetic"})
	if err == nil || !strings.Contains(err.Error(), "storage") {
		t.Fatal("missing shared storage was not rejected before provider resolution", err)
	}
}
