package application

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	sdk "github.com/felinics/twilight/sdk"

	historyfrag "github.com/felinics/memoh/internal/agent/context/history"
	agenttools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/agent/toolexec"
	"github.com/felinics/memoh/internal/agent/turn"
)

func sha256Hex(t *testing.T, payload []byte) string {
	t.Helper()
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// The read_media decoration layer hands the model the media payload in the
// current turn only. Persistence must never carry the bytes: the drained
// internal-feedback user row stores a reference row (content_hash + mime +
// size) instead, matching the identity-only shape every other persisted
// attachment path already uses.

var (
	base64CarrierRe = regexp.MustCompile(`data:[a-zA-Z0-9.+/-]+;base64,[A-Za-z0-9+/=]{32,}`)
	bareBase64RunRe = regexp.MustCompile(`[A-Za-z0-9+/_-]{256,}={0,2}`)
)

func persistedRowContent(t *testing.T, msg turn.ModelMessage) string {
	t.Helper()
	raw, err := historyfrag.MarshalStoredModelMessage(normalizeUserMessageContent(msg))
	if err != nil {
		t.Fatalf("marshal stored message: %v", err)
	}
	return string(raw)
}

// TestPersistedRoundNeverCarriesMediaPayload is the storage invariant: after
// the persistence boundary, no role's bot_history_messages.content contains a
// base64/data: carrier. The round mirrors what a read_media turn produces:
// user query, assistant tool call, tool result (public reference JSON), the
// drained internal-feedback image user message, and the final assistant text.
func TestPersistedRoundNeverCarriesMediaPayload(t *testing.T) {
	t.Parallel()

	pngBytes := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00payload")
	imageBase64 := base64.StdEncoding.EncodeToString(pngBytes)

	round := sdkMessagesWithOrigins([]sdk.Message{
		sdk.UserMessage("look at the image"),
		{
			Role: sdk.MessageRoleAssistant,
			Content: []sdk.MessagePart{sdk.ToolCallPart{
				ToolCallID: "call-1",
				ToolName:   agenttools.ReadMediaToolName().String(),
				Input:      toolexec.ArgumentsFromValue(map[string]any{"path": "images/demo.png"}),
			}},
		},
		{
			Role: sdk.MessageRoleTool,
			Content: []sdk.MessagePart{sdk.ToolResultPart{
				ToolCallID: "call-1",
				ToolName:   agenttools.ReadMediaToolName().String(),
				Result: toolexec.OutputFromValue(agenttools.ReadMediaToolOutput{
					Public: agenttools.ReadMediaToolResult{
						OK:   true,
						Path: "images/demo.png",
						Mime: "image/png",
						Size: len(pngBytes),
					},
					ImageBase64:    imageBase64,
					ImageMediaType: "image/png",
				}.Public),
			}},
		},
		// The drained read_media carrier: one user message whose only part is
		// the image payload, exactly as drainReadMediaMessage injects it.
		{
			Role: sdk.MessageRoleUser,
			Content: []sdk.MessagePart{
				sdk.ImagePart{Image: "data:image/png;base64," + imageBase64, MediaType: "image/png"},
			},
		},
		{Role: sdk.MessageRoleAssistant, Content: []sdk.MessagePart{sdk.TextPart{Text: "done"}}},
	}, []int{3})

	for _, msg := range round {
		content := persistedRowContent(t, msg)
		if base64CarrierRe.MatchString(content) || bareBase64RunRe.MatchString(content) {
			t.Fatalf("role %q persisted a media payload: %.120s", msg.Role, content)
		}
	}
}

// TestReadMediaCarrierPersistsAsReferenceRow pins the persisted form of the
// drained carrier row: a text reference row with content_hash (sha256 of the
// decoded payload), mime, and size — never the bytes.
func TestReadMediaCarrierPersistsAsReferenceRow(t *testing.T) {
	t.Parallel()

	pngBytes := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00payload")
	imageBase64 := base64.StdEncoding.EncodeToString(pngBytes)

	msg := turn.ModelMessage{
		Role:    "user",
		Content: json.RawMessage(`[{"type":"image","image":"data:image/png;base64,` + imageBase64 + `","mediaType":"image/png"}]`),
	}

	content := persistedRowContent(t, msg)

	hash := sha256Hex(t, pngBytes)
	if !strings.Contains(content, hash) {
		t.Fatalf("persisted reference row missing content hash %q: %s", hash, content)
	}
	if strings.Contains(content, imageBase64) {
		t.Fatalf("persisted carrier row kept the payload: %.120s", content)
	}

	decoded := historyfrag.DecodeStoredModelMessage(nil, "row-1", "user", json.RawMessage(content))
	reference := decoded.TextContent()
	wantReference := fmt.Sprintf(`<attachment content_hash="%s" mime="image/png" size="%d"/>`, hash, len(pngBytes))
	if reference != wantReference {
		t.Fatalf("persisted carrier row = %q, want reference row %q", reference, wantReference)
	}
}

// TestHistoryReplayOfReferenceRowCarriesNoPayload exercises the replay
// boundary: decoding the stored row back into SDK messages keeps the
// reference text and yields no image payload for the context assembler.
func TestHistoryReplayOfReferenceRowCarriesNoPayload(t *testing.T) {
	t.Parallel()

	pngBytes := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00payload")
	imageBase64 := base64.StdEncoding.EncodeToString(pngBytes)

	stored := persistedRowContent(t, turn.ModelMessage{
		Role:    "user",
		Content: json.RawMessage(`[{"type":"image","image":"data:image/png;base64,` + imageBase64 + `","mediaType":"image/png"}]`),
	})

	decoded := historyfrag.DecodeStoredModelMessage(nil, "row-1", "user", json.RawMessage(stored))
	replayed := historyfrag.StoredModelMessageToSDKMessage(decoded)

	raw, err := json.Marshal(replayed)
	if err != nil {
		t.Fatalf("marshal replayed message: %v", err)
	}
	if strings.Contains(string(raw), imageBase64) || base64CarrierRe.MatchString(string(raw)) {
		t.Fatalf("replayed context carried the media payload: %.120s", raw)
	}
	hash := sha256Hex(t, pngBytes)
	if !strings.Contains(string(raw), hash) {
		t.Fatalf("replayed context lost the reference identity: %s", raw)
	}
}
