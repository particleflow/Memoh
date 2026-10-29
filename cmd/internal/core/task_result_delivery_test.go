package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/agent/application"
	tools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/agent/turn"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/bots"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/runtimefence"
	"github.com/felinics/memoh/internal/taskresult"
)

// Actual PostgreSQL JSON CAS and fenced history transactions, with a capture
// port in place of model execution. The separate runtime fixture exercises the
// real application/model boundary and process crash, not just this port.
func TestTaskResultPostgresCrashWindows(t *testing.T) {
	dsn := os.Getenv("MEMOH_NATIVE_QUEUE_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL DSN not supplied")
	}
	ctx := context.Background()
	open := func() *pgxpool.Pool {
		p, e := db.OpenPostgresDSN(ctx, dsn)
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	pool := open()
	defer func() { pool.Close() }()
	user, bot, parent, child, source := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO users(id,username,is_active) VALUES($1,$2,true)`, user, "result-fixture-"+user)
	exec(`INSERT INTO team_members(user_id,role) VALUES($1,'member')`, user)
	exec(`INSERT INTO bots(id,owner_user_id,name) VALUES($1,$2,'result-fixture')`, bot, user)
	defer func() {
		exec(`DELETE FROM bots WHERE id=$1`, bot)
		exec(`DELETE FROM team_members WHERE user_id=$1`, user)
		exec(`DELETE FROM users WHERE id=$1`, user)
	}()
	exec(`INSERT INTO bot_sessions(id,bot_id,created_by_user_id,runtime_fencing_token) VALUES($1,$2,$3,1)`, parent, bot, user)
	exec(`INSERT INTO bot_sessions(id,bot_id,created_by_user_id,type,parent_session_id) VALUES($1,$2,$3,'subagent',$4)`, child, bot, user, parent)
	exec(`INSERT INTO session_runs(run_id,bot_id,session_id,invocation_id,turn_id,turn_position,state,input_json,input_fingerprint,fencing_token) VALUES($1,$2,$3,'source',$1,1,'running','{}','source',1)`, source, bot, parent)
	var received []turn.StartTurnCommand
	var mu sync.Mutex
	host := func() *delegationHost {
		store := postgresstore.NewWithPool(pool, sqlc.New(pool))
		a := accounts.NewService(nil, store)
		b := bots.NewService(nil, postgresstore.NewQueriesWithPool(pool, store.SQLC()))
		sessions := thread.NewService(nil, postgresstore.NewQueriesWithPool(pool, store.SQLC()), nil)
		h := &delegationHost{store: store, accounts: a, permissions: &applicationBotPermissionChecker{bots: b, accounts: a}, sessions: sessions, messages: messagepkg.NewService(nil, postgresstore.NewQueriesWithPool(pool, store.SQLC()))}
		h.service = botdelegation.NewService(&botdelegation.Authority{Permissions: h, Sessions: sessions, Workdirs: delegationWorkdirs{store}, Mounts: store, Assets: h}, botdelegation.NewPostgresLedger(store.SQLC()), nil, nil, time.Minute)
		h.service.Host = h
		t.Cleanup(h.service.Close)
		h.resultSubmit = func(_ context.Context, _ string, cmd turn.StartTurnCommand) error {
			mu.Lock()
			defer mu.Unlock()
			received = append(received, cmd)
			return nil
		}
		return h
	}
	h := host()
	q := &nativeTaskQueueHost{host: h}
	fenced := runtimefence.WithContext(ctx, runtimefence.Fence{BotID: bot, SessionID: parent, Token: 1})
	enqueue := func(call string) dbstore.NativeTaskReceipt {
		r, created, e := q.Enqueue(fenced, tools.SessionContext{BotID: bot, SessionID: parent, UserID: user}, call, "worker", child, thread.SubagentConfig{}, call, call)
		if e != nil || !created {
			t.Fatalf("enqueue %v %v", created, e)
		}
		return r
	}
	r := enqueue("saved-result-before-crash")
	r, won, err := q.Claim(ctx, r)
	if err != nil || !won {
		t.Fatal(err)
	}
	worker := uuid.NewString()
	exec(`INSERT INTO session_runs(run_id,bot_id,session_id,invocation_id,turn_id,turn_position,state,input_json,input_fingerprint) VALUES($1,$2,$3,$4,$1,1,'completed','{}','worker')`, worker, bot, child, "subagent:"+r.TaskID)
	if _, err = q.Finish(ctx, r, worker, "completed"); err != nil {
		t.Fatal(err)
	}
	// C1: execution/result are durable, delivery wake and all memory are lost.
	pool.Close()
	pool = open()
	h = host()
	q = &nativeTaskQueueHost{host: h}
	reconcile := func() {
		t.Helper()
		if e := h.reconcileResult(ctx, "native_task", r.TaskID, bot, user, parent); e != nil {
			t.Fatal(e)
		}
	}
	reconcile()
	reconcile()
	if len(received) != 2 || received[0].TaskResultReceipt != received[1].TaskResultReceipt {
		t.Fatal("lost wake changed original invocation")
	}
	first := received[0]
	ref, _ := taskresult.Parse(first.TaskResultReceipt)
	if ref.Attempt != 1 {
		t.Fatal(ref)
	}
	// Simulate accepted summary turns with real run records. Progress is a
	// fenced history step but deliberately has no final-publication receipt.
	position := int64(2)
	admitSummary := func(cmd turn.StartTurnCommand) string {
		run := uuid.NewString()
		inv := application.HostFollowUpInvocation(bot, parent, cmd.TaskResultReceipt)
		exec(`INSERT INTO session_runs(run_id,bot_id,session_id,invocation_id,turn_id,turn_position,state,input_json,input_fingerprint,fencing_token) VALUES($1,$2,$3,$4,$1,$5,'running','{}','summary',1)`, run, bot, parent, inv, position)
		return run
	}
	write := func(run, text, receipt string) string {
		metadata := map[string]any{}
		if receipt != "" {
			metadata[taskresult.MetadataKey] = receipt
		}
		content, _ := json.Marshal(text)
		messages, e := h.messages.PersistAgentStep(fenced, messagepkg.AgentStep{RunID: run, Messages: []messagepkg.PersistInput{{BotID: bot, SessionID: parent, Role: "assistant", RunID: run, TurnID: run, TurnPosition: &position, Content: content, Metadata: metadata}}})
		if e != nil || len(messages) != 1 {
			t.Fatalf("persist step %v", e)
		}
		return messages[0].ID
	}
	unknown := enqueue("unknown-before-admission")
	exec(`UPDATE session_runs SET state='completed' WHERE run_id=$1`, source)
	firstRun := admitSummary(first)
	write(firstRun, "I am preparing the report.", "")
	exec(`UPDATE session_runs SET state='failed' WHERE run_id=$1`, firstRun)
	position++
	reconcile()
	second := received[len(received)-1]
	ref, _ = taskresult.Parse(second.TaskResultReceipt)
	if ref.Attempt != 2 {
		t.Fatalf("C2 progress marked answered: %+v", ref)
	}
	secondRun := admitSummary(second)
	exec(`UPDATE session_runs SET state='completed' WHERE run_id=$1`, secondRun)
	position++
	reconcile()
	third := received[len(received)-1]
	ref, _ = taskresult.Parse(third.TaskResultReceipt)
	if ref.Attempt != 3 {
		t.Fatalf("C2 empty completion marked answered: %+v", ref)
	}
	thirdRun := admitSummary(third)
	messageID := write(thirdRun, "The saved task completed without a final answer.", third.TaskResultReceipt)
	// C3: committed final reply, then lost process/consumption acknowledgement.
	// Even a failed/lost summary run must not erase an already visible reply.
	exec(`UPDATE session_runs SET state='lost' WHERE run_id=$1`, thirdRun)
	before := len(received)
	pool.Close()
	pool = open()
	h = host()
	q = &nativeTaskQueueHost{host: h}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := h.reconcileResult(ctx, "native_task", r.TaskID, bot, user, parent); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if len(received) != before {
		t.Fatal("C3 published summary was repeated")
	}
	task, e := h.loadResultTask(ctx, "native_task", r.TaskID, bot, user, parent)
	if e != nil {
		t.Fatal(e)
	}
	event := task.delivery[ref.EventID]
	if event.MessageID != messageID || event.SummaryRunID != thirdRun {
		t.Fatalf("missing committed proof: %+v", event)
	}
	if _, _, e = h.prepareResult(ctx, application.ChatRequest{TaskResultReceipt: third.TaskResultReceipt, BotID: bot, ThreadID: parent, UserID: user}); !errors.Is(e, botdelegation.ErrDenied) {
		t.Fatal("already reported event can execute again")
	}
	var executions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM session_runs WHERE session_id=$1 AND invocation_id=$2`, child, "subagent:"+r.TaskID).Scan(&executions); err != nil {
		t.Fatal(err)
	}
	if executions != 1 {
		t.Fatalf("external task replayed: %d", executions)
	}
	// Unknown claim has no execution receipt. Startup creates only a report;
	// the original native queue still refuses to claim that action again.
	unknown, won, e = q.Claim(ctx, unknown)
	if e != nil || !won {
		t.Fatal(e)
	}
	if e = h.recoverUnadmittedResults(ctx); e != nil {
		t.Fatal(e)
	}
	if e = h.reconcileResult(ctx, "native_task", unknown.TaskID, bot, user, parent); e != nil {
		t.Fatal(e)
	}
	ut, e := h.loadResultTask(ctx, "native_task", unknown.TaskID, bot, user, parent)
	if e != nil || ut.state != "unknown" {
		t.Fatalf("unknown missing: %v", e)
	}
	if _, won, e = q.Claim(ctx, unknown); e != nil || won {
		t.Fatalf("unknown action reclaimed: %v %v", won, e)
	}
	// A concurrent execution save must not erase the independently committed
	// delivery JSON. Exercise the real generated SQL with the original revision.
	target := uuid.NewString()
	exec(`INSERT INTO bots(id,owner_user_id,name) VALUES($1,$2,'result-target')`, target, user)
	defer exec(`DELETE FROM bots WHERE id=$1`, target)
	delegation := uuid.NewString()
	exec(`INSERT INTO bot_delegations(id,user_id,source_bot_id,source_session_id,source_root_id,target_bot_id,request_id,run_id,fingerprint,source_selection,state,payload) VALUES($1,$2,$3,$4,$4,$5,$1,$1,'result-save','native','unknown','{}')`, delegation, user, bot, parent, target)
	if _, e = h.store.SaveDelegationResultDelivery(ctx, sqlc.SaveDelegationResultDeliveryParams{ID: nativeTaskUUID(delegation), Expected: []byte(`{}`), Delivery: []byte(`{"saved":{"report_state":"reported","message_id":"original"}}`)}); e != nil {
		t.Fatal(e)
	}
	if _, e = h.store.UpdateBotDelegation(ctx, sqlc.UpdateBotDelegationParams{ID: nativeTaskUUID(delegation), UserID: nativeTaskUUID(user), RunID: nativeTaskUUID(delegation), State: "unknown", DeliveryState: "accepted", Result: []byte(`{"reason":"uncertain"}`), Revision: 0}); e != nil {
		t.Fatal(e)
	}
	kept, e := h.store.ReadDelegationResultDelivery(ctx, nativeTaskUUID(delegation))
	if e != nil || !strings.Contains(string(kept), "original") {
		t.Fatal("execution save erased publication receipt")
	}
	// Restart before cross-Bot target admission preserves the original request
	// as unknown, without creating a target session or replacement execution.
	exec(`UPDATE bot_delegations SET state='accepted',payload='{"result_delivery_version":1,"target_selection":"native"}' WHERE id=$1`, delegation)
	if e = h.recoverUnadmittedResults(ctx); e != nil {
		t.Fatal(e)
	}
	unadmitted, e := h.service.ReadRecord(ctx, bot, user, parent, delegation)
	if e != nil || unadmitted.State != "unknown" || unadmitted.Identity.TargetSessionID != "" {
		t.Fatalf("unadmitted delegation was not preserved as unknown: %+v %v", unadmitted, e)
	}
	// Live revocation blocks the publication boundary and all later recovery.
	exec(`UPDATE users SET is_active=false WHERE id=$1`, user)
	if e = h.reconcileResult(ctx, "native_task", unknown.TaskID, bot, user, parent); e == nil {
		t.Fatal("revoked source delivered")
	}
	t.Log("C1 stable invocation; C2 progress/empty output not consumed; C3 8 concurrent recoveries reused committed message; original action count=1; unknown never reclaimed; revocation denied")
}
