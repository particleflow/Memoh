package application

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/agent/runtime/session/ledger"
	"github.com/felinics/memoh/internal/conversationpresence"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/inputsource"
	"github.com/felinics/memoh/internal/runtimefence"
)

func presencePostgresFixture(t *testing.T) (*postgresPresenceClaims, context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pool := openTurnAdmissionPostgres(t, ctx)
	bot, session := createTurnAdmissionFixture(t, ctx, pool)
	q := postgresstore.NewQueriesWithPool(pool, sqlc.New(pool))
	manager := sessionruntime.NewManager(sessionruntime.NewMemoryBackend(), sessionruntime.Options{OwnerID: "presence-fixture-owner", StateTTL: time.Minute, OwnerLeaseTTL: time.Minute, Ledger: ledger.NewPostgres(sqlc.New(pool), pool), Fence: runtimefence.NewActivator(q)})
	t.Cleanup(func() { _ = manager.Close() })
	admitted, err := manager.Admit(ctx, sessionruntime.AdmitInput{BotID: bot, SessionID: session, InvocationID: uuid.NewString(), Payload: []byte(`{"kind":"message","text":"fixture"}`), Execution: sessionruntime.Execution{Admission: func(context.Context, sessionruntime.RunHandle) (sessionruntime.RunAdmissionView, error) {
		return sessionruntime.RunAdmissionView{}, nil
	}, Cancel: func() {}}})
	if err != nil || !admitted.Started {
		t.Fatalf("admission: %v", err)
	}
	actor := uuid.NewString()
	return &postgresPresenceClaims{queries: q, handle: admitted.Handle, actor: actor, source: inputsource.Source{Kind: inputsource.User, BotID: bot, SessionID: session, UserID: actor}, authorize: func(context.Context) (bool, error) { return true, nil }}, ctx, pool
}

func TestConversationPresencePostgresClaimPublishAndRestart(t *testing.T) {
	claims, ctx, pool := presencePostgresFixture(t)
	ticket, facts, inserted, err := claims.prepare(ctx, conversationpresence.ContinuityPolicy{}, false)
	if err != nil || !inserted || !facts.FirstDirectInput {
		t.Fatalf("prepare: inserted=%v facts=%+v error=%v", inserted, facts, err)
	}
	// Two independently constructed application owners share the durable receipt.
	other := *claims
	results := make(chan bool, 2)
	failures := make(chan error, 2)
	var wg sync.WaitGroup
	for _, owner := range []*postgresPresenceClaims{claims, &other} {
		wg.Add(1)
		go func(c *postgresPresenceClaims) {
			defer wg.Done()
			ok, e := c.Claim(ctx, ticket)
			results <- ok
			failures <- e
		}(owner)
	}
	wg.Wait()
	close(results)
	close(failures)
	count := 0
	for ok := range results {
		if ok {
			count++
		}
	}
	for e := range failures {
		if e != nil {
			t.Fatal(e)
		}
	}
	if count != 1 {
		t.Fatalf("durable claims=%d", count)
	}
	piece := conversationpresence.Piece{Kind: "conversation_ack", Text: "收到，我来梳理。", Phase: conversationpresence.Thinking, InputSource: claims.source}
	if ok, e := other.Publish(ctx, ticket, piece); e != nil || !ok {
		t.Fatalf("publish: %v %v", ok, e)
	}
	if ok, e := claims.Publish(ctx, ticket, piece); e != nil || ok {
		t.Fatalf("duplicate publish: %v %v", ok, e)
	}
	if err = other.Close(ctx, ticket, "formal_text"); err != nil {
		t.Fatal(err)
	}
	if _, _, again, e := other.prepare(ctx, conversationpresence.ContinuityPolicy{}, false); e != nil || again {
		t.Fatalf("restart replay: %v %v", again, e)
	}
	if ok, e := claims.Claim(ctx, ticket); e != nil || ok {
		t.Fatalf("closed replay claim: %v %v", ok, e)
	}
	var state string
	var claimed, persisted bool
	var history int
	if e := pool.QueryRow(ctx, `SELECT state, claimed_at IS NOT NULL, piece IS NOT NULL FROM conversation_presence WHERE run_id=$1`, claims.handle.RunID).Scan(&state, &claimed, &persisted); e != nil {
		t.Fatal(e)
	}
	if state != "closed" || !claimed || !persisted {
		t.Fatalf("receipt lost after close: %s %v %v", state, claimed, persisted)
	}
	if e := pool.QueryRow(ctx, `SELECT count(*) FROM bot_history_messages WHERE session_id=$1`, claims.handle.SessionID).Scan(&history); e != nil || history != 0 {
		t.Fatalf("ack polluted history: %d %v", history, e)
	}
}

func TestConversationPresencePostgresCloseBeforePrepare(t *testing.T) {
	claims, ctx, _ := presencePostgresFixture(t)
	if err := claims.Close(ctx, claims.baseTicket(), "formal_first"); err != nil {
		t.Fatal(err)
	}
	if _, _, inserted, e := claims.prepare(ctx, conversationpresence.ContinuityPolicy{}, false); e != nil || inserted {
		t.Fatalf("late prepare: %v %v", inserted, e)
	}
}

func TestConversationPresencePostgresRejectsStaleAndRevoked(t *testing.T) {
	claims, ctx, _ := presencePostgresFixture(t)
	ticket, _, _, err := claims.prepare(ctx, conversationpresence.ContinuityPolicy{}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*postgresPresenceClaims, *conversationpresence.Ticket)
	}{
		{"actor", func(_ *postgresPresenceClaims, t *conversationpresence.Ticket) { t.Scope.ActorID = "foreign" }},
		{"team", func(_ *postgresPresenceClaims, t *conversationpresence.Ticket) { t.Scope.TeamID = uuid.NewString() }},
		{"generation", func(c *postgresPresenceClaims, t *conversationpresence.Ticket) {
			c.handle.Generation = "other"
			t.Generation = "other"
		}},
		{"fence", func(c *postgresPresenceClaims, t *conversationpresence.Ticket) { c.handle.FencingToken++; t.Fence++ }},
		{"owner", func(c *postgresPresenceClaims, _ *conversationpresence.Ticket) { c.handle.OwnerID = "foreign-owner" }},
		{"permission", func(c *postgresPresenceClaims, _ *conversationpresence.Ticket) {
			c.authorize = func(context.Context) (bool, error) { return false, nil }
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			other := *claims
			changed := ticket
			tc.change(&other, &changed)
			ok, e := other.Claim(ctx, changed)
			if ok {
				t.Fatalf("stale claim accepted: %v %v", ok, e)
			}
			if e != nil && !errors.Is(e, runtimefence.ErrStale) && !errors.Is(e, conversationpresence.ErrInvalidContract) {
				t.Fatalf("unexpected rejection: %v", e)
			}
		})
	}
}

func TestConversationPresencePostgresAbortRejectsPublication(t *testing.T) {
	claims, ctx, pool := presencePostgresFixture(t)
	ticket, _, _, err := claims.prepare(ctx, conversationpresence.ContinuityPolicy{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if ok, e := claims.Claim(ctx, ticket); e != nil || !ok {
		t.Fatalf("claim: %v %v", ok, e)
	}
	if _, e := pool.Exec(ctx, `UPDATE session_runs SET abort_requested_at=now() WHERE run_id=$1`, claims.handle.RunID); e != nil {
		t.Fatal(e)
	}
	piece := conversationpresence.Piece{Kind: "conversation_ack", Text: "Late acknowledgement", Phase: conversationpresence.Thinking, InputSource: claims.source}
	if ok, e := claims.Publish(ctx, ticket, piece); ok || !errors.Is(e, runtimefence.ErrStale) {
		t.Fatalf("publication survived abort: %v %v", ok, e)
	}
}

func TestConversationPresencePostgresRevocationStillClosesFeedback(t *testing.T) {
	for _, prepared := range []bool{false, true} {
		name := "before_prepare"
		if prepared {
			name = "after_prepare"
		}
		t.Run(name, func(t *testing.T) {
			claims, ctx, _ := presencePostgresFixture(t)
			ticket := claims.baseTicket()
			if prepared {
				var err error
				ticket, _, _, err = claims.prepare(ctx, conversationpresence.ContinuityPolicy{}, false)
				if err != nil {
					t.Fatal(err)
				}
			}
			if prepared {
				if ok, err := claims.Claim(ctx, ticket); err != nil || !ok {
					t.Fatalf("initial claim: %v %v", ok, err)
				}
			}
			claims.authorize = func(context.Context) (bool, error) { return false, nil }
			if prepared {
				piece := conversationpresence.Piece{Kind: "conversation_ack", Text: "Must be suppressed", Phase: conversationpresence.Thinking, InputSource: claims.source}
				if ok, err := claims.Publish(ctx, ticket, piece); ok || !errors.Is(err, runtimefence.ErrStale) {
					t.Fatalf("revoked publication: %v %v", ok, err)
				}
			}
			foreignOwner := *claims
			foreignOwner.handle.OwnerID = "foreign-owner"
			if err := foreignOwner.Close(ctx, ticket, "wrong_owner"); !errors.Is(err, runtimefence.ErrStale) {
				t.Fatalf("cleanup bypassed owner: %v", err)
			}
			staleFence := *claims
			staleFence.handle.FencingToken++
			staleTicket := ticket
			staleTicket.Fence++
			if err := staleFence.Close(ctx, staleTicket, "wrong_fence"); !errors.Is(err, runtimefence.ErrStale) {
				t.Fatalf("cleanup bypassed fence: %v", err)
			}
			if err := claims.Close(ctx, ticket, "permission_revoked"); err != nil {
				t.Fatalf("trusted fenced cleanup must survive chat revocation: %v", err)
			}
			// A later regrant must not resurrect the invalidated input.
			claims.authorize = func(context.Context) (bool, error) { return true, nil }
			if prepared {
				if ok, err := claims.Claim(ctx, ticket); err != nil || ok {
					t.Fatalf("closed input reclaimed: %v %v", ok, err)
				}
			} else {
				if _, _, inserted, err := claims.prepare(ctx, conversationpresence.ContinuityPolicy{}, false); err != nil || inserted {
					t.Fatalf("close-before-prepare lost: %v %v", inserted, err)
				}
			}
		})
	}
}

func TestConversationPresencePostgresFirstDirectIgnoresValidMachineHistory(t *testing.T) {
	for _, tc := range []struct {
		name      string
		kind      string
		malformed string
		want      conversationpresence.Temperature
	}{
		{"schedule", inputsource.Schedule, "", conversationpresence.Cold},
		{"task result", inputsource.Result, "", conversationpresence.Cold},
		{"delegation", inputsource.Delegation, "", conversationpresence.Cold},
		{"native task", inputsource.NativeTask, "", conversationpresence.Cold},
		{"missing provenance", "", "missing", conversationpresence.Uncertain},
		{"explicit unknown", inputsource.Unknown, "", conversationpresence.Uncertain},
		{"unsupported source", "future_kind", "", conversationpresence.Uncertain},
		{"task missing id", inputsource.Schedule, "task_id", conversationpresence.Uncertain},
		{"foreign bot", inputsource.Result, "bot_id", conversationpresence.Uncertain},
		{"foreign session", inputsource.Result, "session_id", conversationpresence.Uncertain},
		{"wrong field type", inputsource.Schedule, "field_type", conversationpresence.Uncertain},
		{"direct source without actor", inputsource.User, "actor", conversationpresence.Uncertain},
		{"machine mixed with unknown", inputsource.Schedule, "mixed_unknown", conversationpresence.Uncertain},
		{"machine mixed with direct input", inputsource.Result, "mixed_direct", conversationpresence.Hot},
		{"direct user", inputsource.User, "", conversationpresence.Hot},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims, ctx, pool := presencePostgresFixture(t)
			source := map[string]any{"kind": tc.kind, "bot_id": claims.handle.BotID, "session_id": claims.handle.SessionID, "task_id": "synthetic-task"}
			if tc.kind == inputsource.User {
				source["user_id"] = claims.actor
			}
			switch tc.malformed {
			case "task_id":
				delete(source, "task_id")
			case "bot_id":
				source["bot_id"] = uuid.NewString()
			case "session_id":
				source["session_id"] = uuid.NewString()
			case "field_type":
				source["origin_run_id"] = 17
			case "actor":
				delete(source, "user_id")
			}
			metadata := map[string]any{inputsource.MetadataKey: source}
			if tc.malformed == "missing" {
				metadata = map[string]any{}
			}
			raw, err := json.Marshal(metadata)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = pool.Exec(ctx, `INSERT INTO bot_history_messages(bot_id,session_id,role,content,metadata,created_at) VALUES($1,$2,'user','{"role":"user","content":"synthetic machine history"}'::jsonb,$3,now()-interval '1 minute')`, claims.handle.BotID, claims.handle.SessionID, raw); err != nil {
				t.Fatal(err)
			}
			if tc.malformed == "mixed_unknown" || tc.malformed == "mixed_direct" {
				extra := []byte(`{}`)
				if tc.malformed == "mixed_direct" {
					extra, err = json.Marshal(map[string]any{inputsource.MetadataKey: claims.source})
					if err != nil {
						t.Fatal(err)
					}
				}
				if _, err = pool.Exec(ctx, `INSERT INTO bot_history_messages(bot_id,session_id,role,content,metadata,created_at) VALUES($1,$2,'user','{}'::jsonb,$3,now()-interval '30 seconds')`, claims.handle.BotID, claims.handle.SessionID, extra); err != nil {
					t.Fatal(err)
				}
			}
			_, facts, inserted, err := claims.prepare(ctx, conversationpresence.ContinuityPolicy{IdleAfter: 15 * time.Minute, IdleStartsEpisode: true}, false)
			if err != nil || !inserted {
				t.Fatalf("prepare inserted=%v error=%v", inserted, err)
			}
			decision := conversationpresence.Classify(claims.source, claims.handle.BotID, claims.handle.SessionID, facts, conversationpresence.ContinuityPolicy{IdleAfter: 15 * time.Minute, IdleStartsEpisode: true})
			if decision.Temperature != tc.want {
				t.Fatalf("got %s/%s want %s facts=%+v", decision.Temperature, decision.Reason, tc.want, facts)
			}
		})
	}
}

func TestConversationPresenceUnknownHistoryNeedsTrustedAnchor(t *testing.T) {
	anchor := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		created  time.Time
		receipt  time.Time
		reliable bool
	}{
		{"no receipt", anchor.Add(-time.Minute), time.Time{}, false},
		{"unknown before receipt", anchor.Add(-time.Minute), anchor, true},
		{"unknown after receipt", anchor.Add(time.Minute), anchor, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal([]map[string]any{{"source": nil, "created_at": tc.created}})
			if err != nil {
				t.Fatal(err)
			}
			count, last, reliable, err := presenceHistoryContinuity(raw, "fixture-bot", "fixture-session", tc.receipt)
			if err != nil || count != 1 || !last.IsZero() || reliable != tc.reliable {
				t.Fatalf("unknown history: count=%d last=%s reliable=%v error=%v", count, last, reliable, err)
			}
		})
	}
}
