package core

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	runtime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/agent/runtime/session/ledger"
	"github.com/felinics/memoh/internal/followupqueue"
	"github.com/felinics/memoh/internal/testutil/sessionledger"
)

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
