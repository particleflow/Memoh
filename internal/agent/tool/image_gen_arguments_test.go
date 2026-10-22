package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	sdk "github.com/felinics/twilight/sdk"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/agent/toolexec"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/settings"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

type imageArgumentsQueries struct {
	*subagentModelQueries
	imageModel pgtype.UUID
}

func (q *imageArgumentsQueries) GetSettingsByBotID(context.Context, pgtype.UUID) (sqlc.GetSettingsByBotIDRow, error) {
	return sqlc.GetSettingsByBotIDRow{ImageModelID: q.imageModel}, nil
}

func TestImageToolRejectsWithdrawnArgumentsBeforeProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/images/generations") {
			http.Error(w, "unexpected fixture request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(testPNGBytes) + `"}]}`))
	}))
	t.Cleanup(server.Close)
	catalog, modelID, _ := newSubagentModelCatalog(t)
	catalog.models[0].Config, _ = json.Marshal(models.ModelConfig{Compatibilities: []string{models.CompatImageOutput}})
	providerID := catalog.models[0].ProviderID.String()
	provider := catalog.providers[providerID]
	// Exercise the existing Images protocol selection entirely against local HTTP.
	provider.Config, _ = json.Marshal(map[string]string{"api_key": "synthetic-key", "base_url": server.URL + "/api.openai.com/v1"})
	catalog.providers[providerID] = provider
	queries := &imageArgumentsQueries{subagentModelQueries: catalog, imageModel: mustSubagentUUID(t, modelID)}
	store := media.NewService(nil, localfs.NewOriginals(t.TempDir()))
	store.SetRequirePersistence(true)
	p := NewImageGenProvider(nil, settings.NewService(slog.Default(), queries, nil, nil), models.NewService(slog.Default(), queries), queries, store)
	session := SessionContext{BotID: "00000000-0000-0000-0000-000000000001"}
	tools, err := p.Tools(t.Context(), session)
	if err != nil || len(tools) != 1 {
		t.Fatalf("registered tools=%d err=%v", len(tools), err)
	}
	execute := func(t *testing.T, input string) sdk.ToolResultPart {
		t.Helper()
		outcome, err := toolexec.ExecuteTools(t.Context(), []sdk.ToolCall{{ToolCallID: "synthetic-call", ToolName: "generate_image", Input: sdk.ParseToolArguments(input)}}, toolexec.ToolExecOptions{Tools: tools})
		if err != nil || len(outcome.Results) != 1 {
			t.Fatalf("executor results=%d err=%v", len(outcome.Results), err)
		}
		return outcome.Results[0]
	}
	for _, input := range []string{
		`{"prompt":"synthetic","mode":"edit"}`,
		`{"prompt":"synthetic","mode":"result"}`,
		`{"prompt":"synthetic","input_refs":["synthetic-hash"]}`,
		`{"prompt":"synthetic","task_id":"synthetic-task"}`,
		`{"prompt":"synthetic","mode":"edit","input_refs":["synthetic-hash"]}`,
		`{"prompt":"synthetic","unexpected":null}`,
	} {
		t.Run(input, func(t *testing.T) {
			before := calls.Load()
			result := execute(t, input)
			if !result.IsError || !strings.Contains(result.Result.String(), "only prompt and size") {
				t.Errorf("withdrawn arguments were not explicitly rejected: %+v", result)
			}
			if calls.Load() != before {
				t.Errorf("rejected arguments dispatched %d provider requests", calls.Load()-before)
			}
		})
	}
	for _, input := range []string{`{"prompt":"synthetic"}`, `{"prompt":"synthetic","size":"1024x1024"}`} {
		t.Run(input, func(t *testing.T) {
			before := calls.Load()
			result := execute(t, input)
			if result.IsError || calls.Load() != before+1 || !strings.Contains(result.Result.String(), "content_hash") {
				t.Fatalf("valid generation failed: result=%+v calls=%d", result, calls.Load()-before)
			}
		})
	}
}
