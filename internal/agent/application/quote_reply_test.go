package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/chat/message"
)

// fakeQuoteReplyMessages stubs only the one lookup the quote resolution uses.
type fakeQuoteReplyMessages struct {
	message.Service
	rows map[string]message.Message
}

func (f *fakeQuoteReplyMessages) GetByIDBySession(_ context.Context, _ string, messageID string) (message.Message, error) {
	row, ok := f.rows[messageID]
	if !ok {
		return message.Message{}, errors.New("not found")
	}
	return row, nil
}

func quoteReplyService(rows map[string]message.Message) *Service {
	return &Service{messageService: &fakeQuoteReplyMessages{rows: rows}}
}

// A quote target that exists in the session resolves to its sender and a
// preview truncated like the timeline renderer: 100 runes plus an ellipsis,
// never the full body.
func TestResolveQuoteReplyTargetTruncatesPreview(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("字", 250)
	content, err := json.Marshal(ModelMessage{Role: "assistant", Content: newTextContent(long)})
	if err != nil {
		t.Fatal(err)
	}
	service := quoteReplyService(map[string]message.Message{
		"11111111-1111-1111-1111-111111111111": {
			ID:                "11111111-1111-1111-1111-111111111111",
			Role:              "assistant",
			Content:           content,
			SenderDisplayName: "Research Bot",
		},
	})

	role, sender, preview, ok := service.ResolveQuoteReplyTarget(
		context.Background(), "22222222-2222-2222-2222-222222222222", "11111111-1111-1111-1111-111111111111")
	if !ok {
		t.Fatal("expected the in-session quote target to resolve")
	}
	if role != "assistant" || sender != "Research Bot" {
		t.Fatalf("role/sender = %q/%q", role, sender)
	}
	want := strings.Repeat("字", 100) + "…"
	if preview != want {
		t.Fatalf("preview not truncated to the timeline shape: got %d runes, want %d", len([]rune(preview)), len([]rune(want)))
	}
	if strings.Contains(preview, long) {
		t.Fatal("preview inlined the full quoted body")
	}
}

// A quote id that names no message of this session is ignored (ok=false),
// never an error: the turn proceeds without the quote.
func TestResolveQuoteReplyTargetIgnoresForeignID(t *testing.T) {
	t.Parallel()

	service := quoteReplyService(map[string]message.Message{})

	_, _, _, ok := service.ResolveQuoteReplyTarget(
		context.Background(), "22222222-2222-2222-2222-222222222222", "33333333-3333-3333-3333-333333333333")
	if ok {
		t.Fatal("expected a foreign quote id to be ignored")
	}
}

// A user-authored quote target prefers the row's stored sender name.
func TestResolveQuoteReplyTargetUsesUserSender(t *testing.T) {
	t.Parallel()

	content, err := json.Marshal(ModelMessage{Role: "user", Content: newTextContent("earlier question")})
	if err != nil {
		t.Fatal(err)
	}
	service := quoteReplyService(map[string]message.Message{
		"44444444-4444-4444-4444-444444444444": {
			ID:                "44444444-4444-4444-4444-444444444444",
			Role:              "user",
			Content:           content,
			DisplayContent:    "earlier question",
			SenderDisplayName: "Alice",
		},
	})

	role, sender, preview, ok := service.ResolveQuoteReplyTarget(
		context.Background(), "22222222-2222-2222-2222-222222222222", "44444444-4444-4444-4444-444444444444")
	if !ok {
		t.Fatal("expected the quote target to resolve")
	}
	if role != "user" || sender != "Alice" {
		t.Fatalf("role/sender = %q/%q", role, sender)
	}
	if preview != "earlier question" {
		t.Fatalf("preview = %q", preview)
	}
}
