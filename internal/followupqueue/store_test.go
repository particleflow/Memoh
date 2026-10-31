package followupqueue

import (
	"bytes"
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	runtime "github.com/felinics/memoh/internal/agent/runtime/session"
)

func TestPostgresFollowUpRestartIdentityAndClaims(t *testing.T) {
	dsn := os.Getenv("MEMOH_FOLLOW_UP_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL DSN not supplied")
	}
	ctx := context.Background()
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	key := runtime.Key{BotID: uuid.NewString(), SessionID: uuid.NewString()}
	// The table is installed explicitly by the isolated migration step, never by a test against an unknown database.
	defer func() {
		if _, err := pool.Exec(ctx, `DELETE FROM session_follow_up_queues WHERE bot_id=$1 AND session_id=$2`, key.BotID, key.SessionID); err != nil {
			t.Error(err)
		}
	}()
	newBackend := func(secret string) *Backend {
		store, e := New(pool, secret)
		if e != nil {
			t.Fatal(e)
		}
		return NewBackend(runtime.NewMemoryBackend(), store)
	}
	b := newBackend("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	_, _, err = b.Update(ctx, key, func(snapshot runtime.Snapshot, _ bool) (runtime.Snapshot, bool, error) {
		snapshot.BotID = key.BotID
		snapshot.SessionID = key.SessionID
		snapshot.CurrentRunView = &runtime.CurrentRunView{RunID: uuid.NewString(), Status: runtime.RunStatusRunning}
		return snapshot, true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"text":"fixture","command":{"Token":"DO-NOT-STORE-PLAINTEXT"}}`)
	original, err := b.EnqueueFollowUp(ctx, key, uuid.NewString(), "original-http-invocation", payload)
	if err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err = pool.QueryRow(ctx, `SELECT envelope FROM session_follow_up_queues WHERE bot_id=$1 AND session_id=$2`, key.BotID, key.SessionID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("DO-NOT-STORE-PLAINTEXT")) {
		t.Fatal("plaintext credential persisted")
	}
	if _, err = newBackend("YWJjZGVmMDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODk=").Store.Read(ctx, key); err == nil {
		t.Fatal("wrong key became an empty queue")
	}
	if err = b.Close(); err != nil {
		t.Fatal(err)
	}
	b = newBackend("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=") // Empty live heap; original receipt is PostgreSQL-owned.
	_, pending, err := b.PendingQueues(ctx, key, 0)
	if err != nil || len(pending) != 1 || pending[0].ID != original.ID {
		t.Fatalf("restart receipt: %v %v", pending, err)
	}
	replay, err := b.EnqueueFollowUp(ctx, key, uuid.NewString(), original.InvocationID, payload)
	if err != nil || replay.ID != original.ID {
		t.Fatalf("replay without an active run: %v %v", replay.ID, err)
	}
	if _, err = b.EnqueueFollowUp(ctx, key, uuid.NewString(), original.InvocationID, []byte("changed")); !errors.Is(err, runtime.ErrQueueInvocationConflict) {
		t.Fatalf("conflict: %v", err)
	}
	var wg sync.WaitGroup
	claims := make(chan runtime.FollowUpClaimRef, 8)
	failures := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			item, claim, ok, e := b.ClaimNextFollowUp(ctx, key, "source-terminal")
			if e != nil {
				failures <- e
				return
			}
			if !ok || item.ID != original.ID {
				failures <- errors.New("claim identity changed")
				return
			}
			claims <- claim
		}()
	}
	wg.Wait()
	close(claims)
	close(failures)
	for e := range failures {
		t.Fatal(e)
	}
	var first runtime.FollowUpClaimRef
	for claim := range claims {
		if first.ClaimToken == "" {
			first = claim
		}
		if claim != first {
			t.Fatal("concurrent claims changed the original receipt")
		}
	}
	bad := first
	bad.ClaimToken = "stale"
	if err = b.ApplyFollowUp(ctx, key, bad); !errors.Is(err, runtime.ErrQueueInvalidReference) {
		t.Fatalf("stale claim: %v", err)
	}
	if err = b.ApplyFollowUp(ctx, key, first); err != nil {
		t.Fatal(err)
	}
	b = newBackend("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	replay, err = b.EnqueueFollowUp(ctx, key, uuid.NewString(), original.InvocationID, payload)
	if err != nil || replay.Status != runtime.QueueApplied || replay.ID != original.ID {
		t.Fatalf("terminal replay: %v %v", replay, err)
	}

	// Revalidate authority at the actual claim, not just at the old HTTP POST.
	_, _, err = b.Update(ctx, key, func(snapshot runtime.Snapshot, _ bool) (runtime.Snapshot, bool, error) {
		snapshot.CurrentRunView = &runtime.CurrentRunView{RunID: uuid.NewString(), Status: runtime.RunStatusRunning}
		return snapshot, true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	guarded, err := b.EnqueueFollowUp(HTTPOrigin(ctx), key, uuid.NewString(), "guarded", []byte(`{"text":"guarded"}`))
	if err != nil {
		t.Fatal(err)
	}
	denied := errors.New("fixture authority revoked")
	b.AuthorizeHTTP = func(context.Context, runtime.Key, runtime.FollowUpItem) error { return denied }
	_, _, ok, err := b.ClaimNextFollowUp(ctx, key, "after-revocation")
	if !errors.Is(err, denied) || ok {
		t.Fatalf("revocation claim: %v %v", ok, err)
	}
	b.AuthorizeHTTP = func(ctx context.Context, _ runtime.Key, _ runtime.FollowUpItem) error {
		var one int
		return pool.QueryRow(ctx, "SELECT 1").Scan(&one)
	}
	_, guardedClaim, ok, err := b.ClaimNextFollowUp(ctx, key, "after-revocation")
	if err != nil || !ok || guardedClaim.ItemID != guarded.ID {
		t.Fatalf("retained same receipt: %v %v", guardedClaim, err)
	}
	if err = b.RejectFollowUp(ctx, key, guardedClaim, "fixture-rejected"); err != nil {
		t.Fatal(err)
	}

	// No TTL/compaction drops a terminal or unknown receipt.
	err = b.Store.Change(ctx, key, func(state *State) error {
		state.Items[0].CreatedAt = time.Unix(0, 0)
		state.Items = append(state.Items, runtime.FollowUpItem{ID: "unknown-receipt", Status: "unknown", ErrorCode: "queue_admission_unconfirmed"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := b.Store.Read(ctx, key)
	if err != nil || len(state.Items) != 3 || state.Items[2].Status != "unknown" {
		t.Fatalf("preservation: %v %v", state, err)
	}
	_, pending, err = b.PendingQueues(ctx, key, 0)
	if err != nil || len(pending) != 1 || pending[0].Status != "unknown" {
		t.Fatalf("unknown visibility: %v %v", pending, err)
	}
	_, _, ok, err = b.ClaimNextFollowUp(ctx, key, "different-terminal")
	if err != nil || ok {
		t.Fatalf("unknown was replayed: %v %v", ok, err)
	}
}

func TestPostgresFollowUpMutationsPreserveLiveQueueContracts(t *testing.T) {
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
	store, err := New(pool, "")
	if err != nil {
		t.Fatal(err)
	}
	b := NewBackend(runtime.NewMemoryBackend(), store)
	key := runtime.Key{BotID: uuid.NewString(), SessionID: uuid.NewString()}
	defer func() {
		if _, err := pool.Exec(ctx, `DELETE FROM session_follow_up_queues WHERE bot_id=$1 AND session_id=$2`, key.BotID, key.SessionID); err != nil {
			t.Error(err)
		}
	}()
	_, _, err = b.Update(ctx, key, func(s runtime.Snapshot, _ bool) (runtime.Snapshot, bool, error) {
		s.BotID = key.BotID
		s.SessionID = key.SessionID
		s.CurrentRunView = &runtime.CurrentRunView{RunID: uuid.NewString(), Status: runtime.RunStatusRunning, SteerSupported: true}
		return s, true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	enqueue := func(text string) runtime.FollowUpItem {
		t.Helper()
		item, e := b.EnqueueFollowUp(ctx, key, uuid.NewString(), text, []byte(`{"text":"`+text+`"}`))
		if e != nil {
			t.Fatal(e)
		}
		return item
	}
	first, second := enqueue("first"), enqueue("second")
	ordered, err := b.ReorderFollowUp(ctx, key, runtime.FollowUpPendingRef{ItemID: second.ID}, runtime.FollowUpPendingRef{ItemID: first.ID})
	if err != nil || len(ordered) != 2 || ordered[0].ID != second.ID {
		t.Fatalf("reorder: %v %v", ordered, err)
	}
	edited, err := b.UpdateFollowUp(ctx, key, second.ID, []byte(`{"text":"edited"}`))
	if err != nil || edited.ID != second.ID {
		t.Fatal("edit identity", err)
	}
	if err = b.CancelFollowUp(ctx, key, first.ID); err != nil {
		t.Fatal(err)
	}
	promoted, err := b.PromoteFollowUpToSteer(ctx, key, runtime.FollowUpPendingRef{ItemID: second.ID})
	if err != nil || promoted.Steer.ID == "" || string(promoted.Steer.ID) == string(second.ID) {
		t.Fatalf("promote: %v %v", promoted, err)
	}
	replay, err := b.PromoteFollowUpToSteer(ctx, key, runtime.FollowUpPendingRef{ItemID: second.ID})
	if err != nil || replay.Steer.ID != promoted.Steer.ID {
		t.Fatal("promotion replay identity", err)
	}
	steers, pending, err := b.PendingQueues(ctx, key, 0)
	if err != nil || len(steers) != 1 || len(pending) != 0 || !bytes.Equal(steers[0].Payload, edited.Payload) {
		t.Fatalf("promotion queues: %v %v %v", steers, pending, err)
	}
	third := enqueue("unsupported")
	_, _, err = b.Update(ctx, key, func(s runtime.Snapshot, _ bool) (runtime.Snapshot, bool, error) {
		s.CurrentRunView.SteerSupported = false
		return s, true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.PromoteFollowUpToSteer(ctx, key, runtime.FollowUpPendingRef{ItemID: third.ID}); !errors.Is(err, runtime.ErrQueueSteerUnsupported) {
		t.Fatalf("unsupported promotion: %v", err)
	}
	state, err := store.Read(ctx, key)
	if err != nil || len(state.Items) != 3 || itemAt(&state, third.ID).Status != runtime.QueueAccepted || itemAt(&state, first.ID).Status != runtime.QueueCanceled {
		t.Fatal("history or pending input lost", err)
	}
}
