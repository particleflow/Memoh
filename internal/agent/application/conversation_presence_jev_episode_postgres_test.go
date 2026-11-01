package application

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felinics/memoh/internal/agent/runtime/native"
	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/agent/runtime/session/ledger"
	"github.com/felinics/memoh/internal/conversationpresence"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/inputsource"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/reasoning"
	"github.com/felinics/memoh/internal/runtimefence"
)

// presencePriorReceipt seeds a used conversation: the legacy prepare path
// inserts the session's earlier presence row and its episode, then the run is
// settled so the next message sees a prior episode with no run in flight.
func presencePriorReceipt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, first *postgresPresenceClaims) string {
	t.Helper()
	ticket, _, _, inserted, err := first.prepare(ctx, false)
	if err != nil || !inserted {
		t.Fatalf("prior prepare: inserted=%v error=%v", inserted, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE session_runs SET state='completed', updated_at=now() WHERE run_id=$1`, first.handle.RunID); err != nil {
		t.Fatal(err)
	}
	return ticket.EpisodeID
}

func presenceAdmitRun(t *testing.T, ctx context.Context, pool *pgxpool.Pool, botID, sessionID, actor string) *postgresPresenceClaims {
	t.Helper()
	queries := postgresstore.NewQueriesWithPool(pool, sqlc.New(pool))
	manager := sessionruntime.NewManager(sessionruntime.NewMemoryBackend(), sessionruntime.Options{OwnerID: "presence-episode-owner", StateTTL: time.Minute, OwnerLeaseTTL: time.Minute, Ledger: ledger.NewPostgres(sqlc.New(pool), pool), Fence: runtimefence.NewActivator(queries)})
	t.Cleanup(func() { _ = manager.Close() })
	admitted, err := manager.Admit(ctx, sessionruntime.AdmitInput{BotID: botID, SessionID: sessionID, InvocationID: uuid.NewString(), Payload: []byte(`{"kind":"message","text":"follow-up"}`), Execution: sessionruntime.Execution{Admission: func(context.Context, sessionruntime.RunHandle) (sessionruntime.RunAdmissionView, error) {
		return sessionruntime.RunAdmissionView{}, nil
	}, Cancel: func() {}}})
	if err != nil || !admitted.Started {
		t.Fatalf("follow-up admission: %v", err)
	}
	return &postgresPresenceClaims{queries: queries, handle: admitted.Handle, actor: actor, source: inputsource.Source{Kind: inputsource.User, BotID: botID, SessionID: sessionID, UserID: actor}, authorize: func(context.Context) (bool, error) { return true, nil }}
}

// TestConversationPresencePostgresJevFinalizeRefreshesTicketEpisode pins the E1
// contract at the claim boundary: a cold finalize re-points the receipt to
// run_id, so the in-memory ticket must carry that same terminal episode before
// Claim. A hot finalize leaves the merged episode untouched.
func TestConversationPresencePostgresJevFinalizeRefreshesTicketEpisode(t *testing.T) {
	for _, tc := range []struct {
		name      string
		result    *presenceJevResult
		wantFresh bool
	}{
		{"cold", &presenceJevResult{Mode: presenceModeFastReply, Temperature: conversationpresence.Cold, Reason: "jev_fast_reply", Phase: conversationpresence.Thinking}, true},
		{"hot", &presenceJevResult{Mode: presenceModeNoFastReply, Temperature: conversationpresence.Hot, Reason: "jev_no_fast_reply"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first, ctx, pool := presencePostgresFixture(t)
			priorEpisode := presencePriorReceipt(t, ctx, pool, first)
			claims := presenceAdmitRun(t, ctx, pool, first.handle.BotID, first.handle.SessionID, first.actor)
			ticket, _, _, inserted, err := claims.prepareStep1(ctx, false)
			if err != nil || !inserted {
				t.Fatalf("step1: inserted=%v error=%v", inserted, err)
			}
			if ticket.EpisodeID != priorEpisode {
				t.Fatalf("step1 episode=%s want the prior episode %s", ticket.EpisodeID, priorEpisode)
			}
			if err := claims.finalizePresenceDecision(ctx, &ticket, tc.result, 10); err != nil {
				t.Fatalf("step2: %v", err)
			}
			var rowEpisode string
			if err := pool.QueryRow(ctx, `SELECT episode_id::text FROM conversation_presence WHERE run_id=$1`, claims.handle.RunID).Scan(&rowEpisode); err != nil {
				t.Fatal(err)
			}
			if ticket.EpisodeID != rowEpisode {
				t.Fatalf("ticket episode %s does not match the finalized receipt episode %s", ticket.EpisodeID, rowEpisode)
			}
			want := priorEpisode
			if tc.wantFresh {
				want = claims.handle.RunID
			}
			if rowEpisode != want {
				t.Fatalf("receipt episode %s want %s", rowEpisode, want)
			}
		})
	}
}

// TestConversationPresencePostgresJevColdClaimPublishesWithPriorEpisode drives the real
// v3 sidecar against PostgreSQL: a session that already carries an earlier
// presence row gets a cold Jev decision, which must claim, generate and publish
// instead of stopping at not_claimed (incident E1).
func TestConversationPresencePostgresJevColdClaimPublishesWithPriorEpisode(t *testing.T) {
	first, ctx, pool := presencePostgresFixture(t)
	priorEpisode := presencePriorReceipt(t, ctx, pool, first)
	claims := presenceAdmitRun(t, ctx, pool, first.handle.BotID, first.handle.SessionID, first.actor)

	gateway := newFakeJevGateway(t,
		func(int) map[string]any {
			return map[string]any{"reply_mode": answerChoice("reply_mode", "fast_reply", 0.9)}
		},
		nil,
		map[string]any{"direction": answerChoice("direction", "soothe", 0.8), "urgency": map[string]any{"type": "score", "score": 1.0, "confidence": 0.7}},
	)
	ackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprint(w, `{"id":"fixture","object":"chat.completion","model":"e1-presence-fixture","choices":[{"index":0,"message":{"role":"assistant","content":"{\"text\":\"收到，我这就去看。\",\"phase\":\"thinking\"}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":4,"total_tokens":11}}`); err != nil {
			t.Error(err)
		}
	}))
	defer ackServer.Close()

	options := ConversationPresenceOptions{
		Policy:                   conversationpresence.Policy{Limits: conversationpresence.Limits{Deadline: 15 * time.Second, MaxInputRunes: 100, MaxOutputTokens: 100, MaxTextRunes: 100}},
		MaxConcurrentPerProvider: 1,
		Jev: PresenceJevRuntimeOptions{
			Endpoint: gateway.server.URL, Model: "jev-latest", Timeout: 10 * time.Second,
			PullMax: 4, PullRunes: 500, StateMaxRunes: 3200,
			Directions: []string{"soothe", "progress", "eta", "clarify"},
		},
	}
	runtime := &conversationPresenceRuntime{options: options, capacity: &presenceCapacity{limit: 1, active: map[string]int{}}}
	service := &Service{queries: claims.queries, logger: slog.New(slog.DiscardHandler)}

	selected := models.GetResponse{ID: "e1-presence-model-uuid", ModelID: "e1-presence-fixture", Model: models.Model{ProviderID: "e1-presence-provider", ModelID: "e1-presence-fixture", Config: models.ModelConfig{ThinkingMode: reasoning.ModeNone}}}
	provider := sqlc.Provider{ClientType: string(models.ClientTypeOpenAICompletions)}
	written := make(chan struct{})
	close(written)
	model := &presenceResolvedModel{
		model:    selected,
		provider: provider,
		sdk: models.SDKModelConfig{
			ModelID: "e1-presence-fixture", ClientType: string(models.ClientTypeOpenAICompletions),
			BaseURL: ackServer.URL + "/v1", APIKey: "fixture-only", HTTPClient: ackServer.Client(),
		},
		mainWritten: written,
	}
	req := ChatRequest{BotID: claims.handle.BotID, ChatID: claims.handle.BotID, ThreadID: claims.handle.SessionID, RunID: claims.handle.RunID, TurnID: claims.handle.TurnID, RunHandle: claims.handle, UserID: claims.actor, Query: "上次那个整理任务有结论了吗", RawQuery: "上次那个整理任务有结论了吗", UserVisibleText: "上次那个整理任务有结论了吗", CurrentChannel: "local"}
	sideCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	done := make(chan conversationpresence.Outcome, 1)
	service.runConversationPresenceJev(ctx, runtime, claims, req, model, resolvedContext{runConfig: native.RunConfig{Bot: native.BotInfo{Name: "Fixture"}}}, sideCtx, claims.source, done)

	var outcome conversationpresence.Outcome
	select {
	case outcome = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("v3 presence run did not reach a terminal outcome")
	}
	if outcome.Status != "published" || outcome.Piece == nil || outcome.Piece.Text == "" {
		t.Fatalf("cold decision with a prior episode did not publish: status=%s piece=%+v err=%v", outcome.Status, outcome.Piece, outcome.Err)
	}
	var state string
	var claimed, published bool
	var pieceText, receiptEpisode string
	if err := pool.QueryRow(ctx, `SELECT state, claimed_at IS NOT NULL, piece IS NOT NULL, coalesce(piece->>'text',''), episode_id::text FROM conversation_presence WHERE run_id=$1`, claims.handle.RunID).Scan(&state, &claimed, &published, &pieceText, &receiptEpisode); err != nil {
		t.Fatal(err)
	}
	if state != "published" || !claimed || !published || pieceText == "" {
		t.Fatalf("receipt after cold publish: state=%s claimed=%v piece=%v text=%q", state, claimed, published, pieceText)
	}
	if receiptEpisode != claims.handle.RunID {
		t.Fatalf("cold receipt episode=%s want run_id %s", receiptEpisode, claims.handle.RunID)
	}
	var priorRowEpisode string
	if err := pool.QueryRow(ctx, `SELECT episode_id::text FROM conversation_presence WHERE run_id=$1`, first.handle.RunID).Scan(&priorRowEpisode); err != nil {
		t.Fatal(err)
	}
	if priorRowEpisode != priorEpisode {
		t.Fatalf("earlier receipt episode changed: %s -> %s", priorEpisode, priorRowEpisode)
	}
}
