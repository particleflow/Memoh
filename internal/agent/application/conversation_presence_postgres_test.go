package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/agent/runtime/session/ledger"
	"github.com/felinics/memoh/internal/conversationpresence"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/inputsource"
	"github.com/felinics/memoh/internal/runtimefence"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
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
		{"actor", func(c *postgresPresenceClaims, t *conversationpresence.Ticket) { t.Scope.ActorID = "foreign" }},
		{"team", func(c *postgresPresenceClaims, t *conversationpresence.Ticket) { t.Scope.TeamID = uuid.NewString() }},
		{"generation", func(c *postgresPresenceClaims, t *conversationpresence.Ticket) {
			c.handle.Generation = "other"
			t.Generation = "other"
		}},
		{"fence", func(c *postgresPresenceClaims, t *conversationpresence.Ticket) { c.handle.FencingToken++; t.Fence++ }},
		{"owner", func(c *postgresPresenceClaims, t *conversationpresence.Ticket) { c.handle.OwnerID = "foreign-owner" }},
		{"permission", func(c *postgresPresenceClaims, t *conversationpresence.Ticket) {
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
