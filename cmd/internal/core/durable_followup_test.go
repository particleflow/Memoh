package core

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	runtime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/agent/runtime/session/ledger"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/followupqueue"
	"github.com/felinics/memoh/internal/testutil/sessionledger"
)

type followUpRecoveryLookupHook struct {
	ledger.Store
	after func(context.Context) error
}

func (s followUpRecoveryLookupHook) GetByInvocation(ctx context.Context, sessionID, invocationID string) (ledger.Run, error) {
	run, err := s.Store.GetByInvocation(ctx, sessionID, invocationID)
	if errors.Is(err, ledger.ErrRunNotFound) && s.after != nil {
		if hookErr := s.after(ctx); hookErr != nil {
			return ledger.Run{}, hookErr
		}
	}
	return run, err
}

func TestDurableFollowUpRecoverySinglePostgresConnection(t *testing.T) {
	dsn := os.Getenv("MEMOH_FOLLOW_UP_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL DSN not supplied")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 1
	config.AfterConnect = db.SetDefaultTeamOnConnect
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, mode := range []string{"missing_admission", "claim_changed_after_lookup", "lookup_failed"} {
		t.Run(mode, func(t *testing.T) {
			// This is an observation deadline for the regression, not a product timeout.
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			store, e := followupqueue.New(pool, "")
			if e != nil {
				t.Fatal(e)
			}
			key := runtime.Key{BotID: uuid.NewString(), SessionID: uuid.NewString()}
			defer func() {
				if _, cleanupErr := pool.Exec(context.Background(), `DELETE FROM session_follow_up_queues WHERE bot_id=$1 AND session_id=$2`, key.BotID, key.SessionID); cleanupErr != nil {
					t.Error(cleanupErr)
				}
			}()
			itemID := runtime.FollowUpItemID(uuid.NewString())
			claim := runtime.FollowUpClaimRef{ItemID: itemID, TriggerRunID: uuid.NewString(), ClaimToken: uuid.NewString()}
			if e = store.Change(ctx, key, func(state *followupqueue.State) error {
				state.Items = []runtime.FollowUpItem{{ID: itemID, Status: runtime.QueueClaimed, Claim: &claim, Payload: []byte(`{"text":"fixture"}`)}}
				state.Claims[claim.TriggerRunID] = string(itemID)
				return nil
			}); e != nil {
				t.Fatal(e)
			}
			runs := followUpRecoveryLookupHook{Store: ledger.NewPostgres(sqlc.New(pool), pool)}
			lookupFailure := errors.New("fixture ledger read unavailable")
			if mode == "lookup_failed" {
				runs.after = func(context.Context) error { return lookupFailure }
			}
			if mode == "claim_changed_after_lookup" {
				runs.after = func(ctx context.Context) error {
					return store.Change(ctx, key, func(state *followupqueue.State) error {
						state.Items[0].Claim.ClaimToken = "replacement-claim"
						return nil
					})
				}
			}
			backend := followupqueue.NewBackend(runtime.NewMemoryBackend(), store)
			e = reconcileFollowUpClaims(ctx, backend, key, runs)
			if mode == "lookup_failed" {
				if !errors.Is(e, lookupFailure) {
					t.Fatalf("ledger error not preserved: %v", e)
				}
			} else if e != nil {
				t.Fatalf("single-connection recovery failed: %v", e)
			}
			state, e := store.Read(ctx, key)
			if e != nil || len(state.Items) != 1 {
				t.Fatalf("read original item: %v", e)
			}
			want := runtime.QueueClaimed
			if mode == "missing_admission" {
				want = "unknown"
			}
			if state.Items[0].ID != itemID || state.Items[0].Status != want {
				t.Fatalf("recovery changed identity or newer claim: %#v", state.Items[0])
			}
			if mode == "claim_changed_after_lookup" && state.Items[0].Claim.ClaimToken != "replacement-claim" {
				t.Fatal("newer claim overwritten")
			}
			if pool.Stat().AcquiredConns() != 0 {
				t.Fatal("recovery retained a pool connection")
			}
		})
	}
}

func TestDurableFollowUpRecoveryKeepsUnknownAndOriginalLostRun(t *testing.T) {
	dsn := os.Getenv("MEMOH_FOLLOW_UP_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL DSN not supplied")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store, err := followupqueue.New(pool, "")
	if err != nil {
		t.Fatal(err)
	}
	key := runtime.Key{BotID: uuid.NewString(), SessionID: uuid.NewString()}
	defer func() {
		if _, err := pool.Exec(ctx, `DELETE FROM session_follow_up_queues WHERE bot_id=$1 AND session_id=$2`, key.BotID, key.SessionID); err != nil {
			t.Error(err)
		}
	}()
	backend := followupqueue.NewBackend(runtime.NewMemoryBackend(), store)
	runs := sessionledger.New()
	original, created, err := runs.Admit(ctx, ledger.AdmitParams{RunID: uuid.NewString(), BotID: key.BotID, SessionID: key.SessionID, InvocationID: "follow-up:already-admitted", TurnID: uuid.NewString(), Input: []byte(`{}`), InputFingerprint: "original"})
	if err != nil || !created {
		t.Fatal(err)
	}
	runs.Runs[original.RunID].State = ledger.StateLost
	err = store.Change(ctx, key, func(state *followupqueue.State) error {
		for _, id := range []string{"not-admitted", "already-admitted"} {
			claim := runtime.FollowUpClaimRef{ItemID: runtime.FollowUpItemID(id), TriggerRunID: uuid.NewString(), ClaimToken: uuid.NewString()}
			state.Items = append(state.Items, runtime.FollowUpItem{ID: claim.ItemID, Status: runtime.QueueClaimed, Claim: &claim, Payload: []byte(`{"text":"fixture"}`)})
			state.Claims[claim.TriggerRunID] = id
		}
		state.Items = append(state.Items, runtime.FollowUpItem{ID: "old-unknown", Status: "unknown", ErrorCode: "original-unresolved", Payload: []byte(`{"text":"fixture"}`)})
		state.Items = append(state.Items, runtime.FollowUpItem{ID: "promoted", Status: runtime.QueueCanceled, Payload: []byte(`{"text":"fixture"}`)})
		state.Promotions["promoted"] = runtime.PromoteFollowUpResult{FollowUp: runtime.FollowUpPendingRef{ItemID: "promoted"}, Steer: runtime.SteerItem{ID: "original-steer", TargetRunID: "original-target", Status: runtime.QueueAccepted}}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = reconcileFollowUpClaims(ctx, backend, key, runs); err != nil {
			t.Fatal(err)
		}
	}
	state, err := store.Read(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Items) != 4 || state.Items[0].Status != "unknown" || state.Items[1].Status != runtime.QueueApplied || state.Items[2].ErrorCode != "original-unresolved" {
		t.Fatalf("recovery classification: %#v", state.Items)
	}
	if state.Items[3].Status != "unknown" {
		t.Fatal("dead-generation promotion acceptance was treated as execution")
	}
	if _, err = backend.PromoteFollowUpToSteer(ctx, key, runtime.FollowUpPendingRef{ItemID: "promoted"}); err == nil {
		t.Fatal("unknown promotion replayed")
	}
	if len(runs.Runs) != 1 || runs.Runs[original.RunID].State != ledger.StateLost {
		t.Fatal("recovery changed/replaced the original lost run")
	}
	// Plain HTTP envelopes require no new key. A credential-bearing payload must
	// fail before committing instead of silently writing the token in plaintext.
	err = store.Change(ctx, key, func(state *followupqueue.State) error {
		state.Items = append(state.Items, runtime.FollowUpItem{ID: "secret", Payload: []byte(`{"command":{"Token":"never-plaintext"}}`)})
		return nil
	})
	if err == nil {
		t.Fatal("missing encryption capability accepted credential payload")
	}
	state, err = store.Read(ctx, key)
	if err != nil || len(state.Items) != 4 {
		t.Fatal("failed write replaced prior receipts", err)
	}
}
