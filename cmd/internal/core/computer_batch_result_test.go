package core

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/agent/application"
	"github.com/felinics/memoh/internal/agent/turn"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/bots"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/taskresult"
	"github.com/felinics/memoh/internal/team"
)

// A batch reports once for all its items. The scan must publish exactly one
// aggregate turn after the batch completes, and the per-operation scan must not
// also publish one turn per batch-owned item.
func TestComputerBatchResultPublishesOncePostgres(t *testing.T) {
	dsn := os.Getenv("MEMOH_NATIVE_QUEUE_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL DSN not supplied")
	}
	ctx := context.Background()
	pool, err := db.OpenPostgresDSN(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	user, bot, session, target := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO users(id,username,is_active) VALUES($1,$2,true)`, user, "computer-batch-"+user)
	exec(`INSERT INTO team_members(user_id,role) VALUES($1,'member')`, user)
	exec(`INSERT INTO bots(id,owner_user_id,name) VALUES($1,$2,'computer-batch')`, bot, user)
	exec(`INSERT INTO bot_sessions(id,bot_id,created_by_user_id,runtime_fencing_token) VALUES($1,$2,$3,1)`, session, bot, user)
	exec(`SELECT set_config('memoh.team_id',$1,false)`, team.DefaultTeamID)
	defer func() {
		exec(`DELETE FROM computer_task_batches WHERE bot_id=$1`, bot)
		exec(`DELETE FROM computer_operations WHERE bot_id=$1`, bot)
		exec(`DELETE FROM bots WHERE id=$1`, bot)
		exec(`DELETE FROM team_members WHERE user_id=$1`, user)
		exec(`DELETE FROM users WHERE id=$1`, user)
	}()

	batch := uuid.NewString()
	// One batch-owned operation that is already terminal: it must not produce
	// its own per-operation result turn.
	owned := uuid.NewString()
	items, _ := json.Marshal([]map[string]any{
		{"spec": map[string]any{"kind": "herdr_pi_task", "task": "synthetic first", "model": "synthetic-model"}, "state": "succeeded", "operation_id": owned, "outcome": "succeeded"},
		{"spec": map[string]any{"kind": "herdr_pi_task", "task": "synthetic second", "model": "synthetic-model"}, "state": "failed", "operation_id": uuid.NewString(), "outcome": "failed", "error": "failed"},
	})
	exec(`INSERT INTO computer_task_batches(id,bot_id,session_id,user_id,target_id,run_id,label,fingerprint,state,items) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'completed',$9)`,
		batch, bot, session, user, target, "run-batch-report", "synthetic batch", "fp-"+batch, items)
	details, _ := json.Marshal(map[string]any{"kind": "herdr_pi_task", "batch_owner": batch, "batch": map[string]any{"progress": map[string]any{}, "result": map[string]any{"outcome": "succeeded"}}})
	exec(`INSERT INTO computer_operations(id,bot_id,session_id,user_id,target_id,run_id,fingerprint,target_fingerprint,state,details) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'completed',$9)`,
		owned, bot, session, user, target, "run-item", "f-"+owned, "tf-"+owned, details)

	var received []turn.StartTurnCommand
	var mu sync.Mutex
	store := postgresstore.NewWithPool(pool, sqlc.New(pool))
	acc := accounts.NewService(nil, store)
	botsService := bots.NewService(nil, postgresstore.NewQueriesWithPool(pool, store.SQLC()))
	sessions := thread.NewService(nil, postgresstore.NewQueriesWithPool(pool, store.SQLC()), nil)
	h := &delegationHost{store: store, accounts: acc, permissions: &applicationBotPermissionChecker{bots: botsService, accounts: acc}, sessions: sessions, messages: messagepkg.NewService(nil, postgresstore.NewQueriesWithPool(pool, store.SQLC()))}
	h.service = botdelegation.NewService(&botdelegation.Authority{Permissions: h, Sessions: sessions, Workdirs: delegationWorkdirs{store}, Mounts: store, Assets: h}, botdelegation.NewPostgresLedger(store.SQLC()), nil, nil, time.Minute)
	h.service.Host = h
	t.Cleanup(h.service.Close)
	h.resultSubmit = func(_ context.Context, _ string, cmd turn.StartTurnCommand) error {
		mu.Lock()
		defer mu.Unlock()
		received = append(received, cmd)
		return nil
	}

	for range 3 {
		if err = h.scanResults(ctx); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	delivered := append([]turn.StartTurnCommand(nil), received...)
	mu.Unlock()
	// The fixture's submit is a sink: it records the command but writes no
	// publication row, so an unchanged event is offered again on every pass.
	// The requirement is one aggregate event, never a per-item event.
	events := map[string]bool{}
	for _, cmd := range delivered {
		ref, parseErr := taskresult.Parse(cmd.TaskResultReceipt)
		if parseErr != nil || ref.Kind != "computer_batch" || ref.TaskID != batch {
			t.Fatalf("per-item or foreign event published: %q", cmd.TaskResultReceipt)
		}
		events[ref.EventID] = true
	}
	if len(events) != 1 {
		t.Fatalf("batch produced %d distinct events, want one aggregate: %+v", len(events), delivered)
	}
	if len(delivered) == 0 {
		t.Fatal("batch published no aggregate turn")
	}
	if !strings.Contains(delivered[0].Query, "2 items, 1 succeeded, 1 failed") {
		t.Fatalf("aggregate body=%q", delivered[0].Query)
	}
	if !strings.Contains(delivered[0].Query, owned) {
		t.Fatal("aggregate body omitted the item receipt id")
	}
}

// prepareResult must accept the aggregate receipt it just created and must not
// accept a forged one.
func TestComputerBatchResultPrepareIsReceiptBoundPostgres(t *testing.T) {
	dsn := os.Getenv("MEMOH_NATIVE_QUEUE_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL DSN not supplied")
	}
	ctx := context.Background()
	pool, err := db.OpenPostgresDSN(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	user, bot, session, target := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO users(id,username,is_active) VALUES($1,$2,true)`, user, "computer-batch-prepare-"+user)
	exec(`INSERT INTO team_members(user_id,role) VALUES($1,'member')`, user)
	exec(`INSERT INTO bots(id,owner_user_id,name) VALUES($1,$2,'computer-batch-prepare')`, bot, user)
	exec(`INSERT INTO bot_sessions(id,bot_id,created_by_user_id,runtime_fencing_token) VALUES($1,$2,$3,1)`, session, bot, user)
	exec(`SELECT set_config('memoh.team_id',$1,false)`, team.DefaultTeamID)
	defer func() {
		exec(`DELETE FROM computer_task_batches WHERE bot_id=$1`, bot)
		exec(`DELETE FROM bots WHERE id=$1`, bot)
		exec(`DELETE FROM team_members WHERE user_id=$1`, user)
		exec(`DELETE FROM users WHERE id=$1`, user)
	}()
	batch := uuid.NewString()
	items, _ := json.Marshal([]map[string]any{
		{"spec": map[string]any{"kind": "herdr_pi_task", "task": "synthetic only", "model": "synthetic-model"}, "state": "succeeded", "operation_id": uuid.NewString(), "outcome": "succeeded"},
	})
	exec(`INSERT INTO computer_task_batches(id,bot_id,session_id,user_id,target_id,run_id,fingerprint,state,items) VALUES($1,$2,$3,$4,$5,$6,$7,'completed',$8)`,
		batch, bot, session, user, target, "run-batch-prepare", "fp-"+batch, items)

	var received []turn.StartTurnCommand
	var mu sync.Mutex
	store := postgresstore.NewWithPool(pool, sqlc.New(pool))
	acc := accounts.NewService(nil, store)
	botsService := bots.NewService(nil, postgresstore.NewQueriesWithPool(pool, store.SQLC()))
	sessions := thread.NewService(nil, postgresstore.NewQueriesWithPool(pool, store.SQLC()), nil)
	h := &delegationHost{store: store, accounts: acc, permissions: &applicationBotPermissionChecker{bots: botsService, accounts: acc}, sessions: sessions, messages: messagepkg.NewService(nil, postgresstore.NewQueriesWithPool(pool, store.SQLC()))}
	h.service = botdelegation.NewService(&botdelegation.Authority{Permissions: h, Sessions: sessions, Workdirs: delegationWorkdirs{store}, Mounts: store, Assets: h}, botdelegation.NewPostgresLedger(store.SQLC()), nil, nil, time.Minute)
	h.service.Host = h
	t.Cleanup(h.service.Close)
	h.resultSubmit = func(_ context.Context, _ string, cmd turn.StartTurnCommand) error {
		mu.Lock()
		defer mu.Unlock()
		received = append(received, cmd)
		return nil
	}
	if err = h.scanResults(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	first := received[0]
	mu.Unlock()
	if first.TaskResultReceipt == "" {
		t.Fatal("aggregate turn carried no receipt")
	}
	request := application.ChatRequest{TaskResultReceipt: first.TaskResultReceipt, BotID: bot, ThreadID: session, UserID: user, CurrentChannel: first.CurrentChannel, ChatID: first.ChatID, RouteID: first.RouteID, ReplyTarget: first.ReplyTarget, ConversationType: first.ConversationType}
	if _, prepared, err := h.prepareResult(ctx, request); err != nil {
		t.Fatalf("aggregate receipt refused: %v", err)
	} else if !strings.Contains(prepared.ModelQuery, "1 items, 1 succeeded") {
		t.Fatalf("prepared query=%q", prepared.ModelQuery)
	}
	// A receipt naming another batch must be refused.
	forged := request
	ref, _ := taskresult.Parse(first.TaskResultReceipt)
	ref.TaskID = uuid.NewString()
	forged.TaskResultReceipt = ref.String()
	if _, _, err = h.prepareResult(ctx, forged); err == nil {
		t.Fatal("forged batch receipt accepted")
	}
}
