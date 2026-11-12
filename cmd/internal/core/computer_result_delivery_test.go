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
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/inputsource"
	"github.com/felinics/memoh/internal/runtimefence"
	"github.com/felinics/memoh/internal/taskresult"
	"github.com/felinics/memoh/internal/toolcontext"
)

// A PC task that reached its terminal Pi observation while its Bot
// conversation is idle must flow through the same result-delivery path as
// native tasks: scan -> receipt -> authorized follow-up, once per publication.
func TestComputerTaskResultFollowUpPostgres(t *testing.T) {
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
	user, bot, session, child := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO users(id,username,is_active) VALUES($1,$2,true)`, user, "computer-result-"+user)
	exec(`INSERT INTO team_members(user_id,role) VALUES($1,'member')`, user)
	exec(`INSERT INTO bots(id,owner_user_id,name) VALUES($1,$2,'computer-result')`, bot, user)
	exec(`INSERT INTO bot_sessions(id,bot_id,created_by_user_id,runtime_fencing_token) VALUES($1,$2,$3,1)`, session, bot, user)
	exec(`INSERT INTO bot_sessions(id,bot_id,created_by_user_id,type,parent_session_id) VALUES($1,$2,$3,'subagent',$4)`, child, bot, user, session)
	defer func() {
		exec(`DELETE FROM computer_operations WHERE bot_id=$1`, bot)
		exec(`DELETE FROM bots WHERE id=$1`, bot)
		exec(`DELETE FROM team_members WHERE user_id=$1`, user)
		exec(`DELETE FROM users WHERE id=$1`, user)
	}()
	target := uuid.NewString()
	insertOperation := func(owner, onTarget, state, outcome string) string {
		id, run := uuid.NewString(), uuid.NewString()
		payload := map[string]any{"kind": "herdr_pi_task", "phase": "pi_exited_result_pending", "batch": map[string]any{"progress": map[string]any{}}}
		if outcome != "" {
			payload = map[string]any{
				"kind": "herdr_pi_task", "phase": "pi_" + outcome, "output": "Pi finished the report.", "exit_code": 0,
				"batch": map[string]any{
					"result": map[string]any{"outcome": outcome, "output": "Pi finished the report."},
					"progress": map[string]any{"producer": map[string]any{
						"version": 2,
						"task": map[string]any{
							"operation_id": id, "bot_id": bot, "user_id": user, "session_id": owner, "target_id": onTarget, "run_id": run,
							"attempt": strings.Repeat("a", 32), "generation": strings.Repeat("b", 32), "root_hash": strings.Repeat("c", 64),
						},
						"artifacts": []any{map[string]any{"source_ref": "report_md", "mime": "text/markdown"}},
					}},
				},
			}
		}
		raw, _ := json.Marshal(payload)
		exec(`INSERT INTO computer_operations(id,bot_id,session_id,user_id,target_id,run_id,fingerprint,target_fingerprint,state,details) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			id, bot, owner, user, onTarget, run, "f-"+id, "tf-"+id, state, raw)
		return id
	}
	operation := insertOperation(session, target, "completed", "succeeded")
	// The outcome happened a week before anyone reconnects: the published receipt
	// must keep this time instead of the delivery time.
	occurredAt := time.Date(2026, 11, 3, 15, 9, 0, 0, time.UTC)
	exec(`UPDATE computer_operations SET updated_at=$2 WHERE id=$1`, operation, occurredAt)
	subagentOperation := insertOperation(child, target, "completed", "succeeded")

	var received []turn.StartTurnCommand
	var mu sync.Mutex
	newHost := func() *delegationHost {
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
	h := newHost()

	// Red on the base scan: a terminal PC task produced no follow-up at all.
	if e := h.scanResults(ctx); e != nil {
		t.Fatal(e)
	}
	mu.Lock()
	if len(received) != 1 {
		mu.Unlock()
		t.Fatalf("terminal computer task produced %d follow-ups, want 1", len(received))
	}
	first := received[0]
	mu.Unlock()
	ref, e := taskresult.Parse(first.TaskResultReceipt)
	if e != nil || ref.Kind != "computer_task" || ref.TaskID != operation {
		t.Fatalf("receipt: %+v %v", ref, e)
	}
	if first.ThreadID != session || first.BotID != bot || first.CurrentChannel != "web" || first.DelegationResultID != "" {
		t.Fatalf("command routed wrong: %+v", first)
	}
	if !strings.Contains(first.Query, "Pi outcome: succeeded") || !strings.Contains(first.Query, "report_md") {
		t.Fatalf("receipt text: %q", first.Query)
	}
	_, prepared, e := h.prepareResult(ctx, application.ChatRequest{TaskResultReceipt: first.TaskResultReceipt, BotID: bot, ThreadID: session, UserID: user, CurrentChannel: first.CurrentChannel, ChatID: first.ChatID, RouteID: first.RouteID, ReplyTarget: first.ReplyTarget, ConversationType: first.ConversationType})
	if e != nil || prepared.InputSource == nil || prepared.InputSource.Kind != inputsource.Result || prepared.InputSource.TaskKind != "computer_task" || prepared.InputSource.TaskID != operation || prepared.InputSource.OriginSessionID != session {
		t.Fatalf("prepared result source: %+v %v", prepared.InputSource, e)
	}
	if !strings.Contains(prepared.ModelQuery, "Report this saved background task outcome") || !strings.Contains(prepared.ModelQuery, "do not execute, retry") {
		t.Fatalf("prepared model query: %q", prepared.ModelQuery)
	}
	// The delivered result states when it happened. On the base it carried only
	// the admission time, so a week-old outcome read as today's news.
	if !prepared.TaskResultAt.Equal(occurredAt) {
		t.Fatalf("result time = %v, want the recorded outcome time %v", prepared.TaskResultAt, occurredAt)
	}
	if prepared.TaskResultAt.Equal(time.Now()) || time.Since(prepared.TaskResultAt) < time.Hour {
		t.Fatalf("result time followed the delivery instead of the outcome: %v", prepared.TaskResultAt)
	}

	// A subagent child session's task never reconnects by itself.
	if _, e = h.loadResultTask(ctx, "computer_task", subagentOperation, bot, user, child); e == nil {
		t.Fatal("subagent-owned computer task was deliverable")
	}

	// A lost consumption acknowledgement re-requests the same identity only.
	if e = h.scanResults(ctx); e != nil {
		t.Fatal(e)
	}
	mu.Lock()
	if len(received) != 2 || received[1].TaskResultReceipt != first.TaskResultReceipt {
		mu.Unlock()
		t.Fatal("retry changed the original invocation")
	}
	mu.Unlock()
	// A retry must not re-date the result: the event froze the task's recorded
	// outcome time, and neither this pass nor the delivery bookkeeping moves it.
	_, redelivered, e := h.prepareResult(ctx, application.ChatRequest{TaskResultReceipt: first.TaskResultReceipt, BotID: bot, ThreadID: session, UserID: user, CurrentChannel: first.CurrentChannel, ChatID: first.ChatID, RouteID: first.RouteID, ReplyTarget: first.ReplyTarget, ConversationType: first.ConversationType})
	if e != nil {
		t.Fatal(e)
	}
	if !redelivered.TaskResultAt.Equal(occurredAt) {
		t.Fatalf("redelivered result time = %v, want the recorded outcome time %v", redelivered.TaskResultAt, occurredAt)
	}
	// A committed visible reply ends automatic delivery.
	fenced := runtimefence.WithContext(ctx, runtimefence.Fence{BotID: bot, SessionID: session, Token: 1})
	summaryRun := uuid.NewString()
	invocation := application.HostFollowUpInvocation(bot, session, first.TaskResultReceipt)
	exec(`INSERT INTO session_runs(run_id,bot_id,session_id,invocation_id,turn_id,turn_position,state,input_json,input_fingerprint,fencing_token) VALUES($1,$2,$3,$4,$1,2,'running','{}','summary',1)`, summaryRun, bot, session, invocation)
	position := int64(2)
	metadata := map[string]any{taskresult.MetadataKey: first.TaskResultReceipt}
	content, _ := json.Marshal("The PC task finished: Pi succeeded.")
	messages, e := h.messages.PersistAgentStep(fenced, messagepkg.AgentStep{RunID: summaryRun, Messages: []messagepkg.PersistInput{{BotID: bot, SessionID: session, Role: "assistant", RunID: summaryRun, TurnID: summaryRun, TurnPosition: &position, Content: content, Metadata: metadata}}})
	if e != nil || len(messages) != 1 {
		t.Fatalf("persist summary %v", e)
	}
	exec(`UPDATE session_runs SET state='completed' WHERE run_id=$1`, summaryRun)
	for range 3 {
		if e = h.scanResults(ctx); e != nil {
			t.Fatal(e)
		}
	}
	mu.Lock()
	if len(received) != 2 {
		mu.Unlock()
		t.Fatalf("published summary was repeated: %d", len(received))
	}
	mu.Unlock()

	// An outcome first observed inside a live turn of the owning conversation
	// is reported by that turn; the event is pinned as attention, not re-enqueued.
	inTurn := insertOperation(session, target, "completed", "failed")
	record, e := h.store.ReadComputerOperation(ctx, dbstore.ComputerOperationRecord{ID: inTurn, BotID: bot, SessionID: session, UserID: user})
	if e != nil {
		t.Fatal(e)
	}
	var observed computertask.Details
	if e = json.Unmarshal(record.Details, &observed); e != nil {
		t.Fatal(e)
	}
	h.computerTerminalObserved(fenced, record, observed)
	delivery, e := h.store.ReadComputerResultDelivery(ctx, nativeTaskUUID(inTurn))
	if e != nil || !strings.Contains(string(delivery), "attention") {
		t.Fatalf("in-turn observation not pinned: %s %v", delivery, e)
	}
	for range 2 {
		if e = h.scanResults(ctx); e != nil {
			t.Fatal(e)
		}
	}
	mu.Lock()
	attentionCount := len(received)
	mu.Unlock()
	if attentionCount != 2 {
		t.Fatalf("in-turn observation re-enqueued: %d", attentionCount)
	}

	// An endpoint delivery commit wakes a pull of the still-unfinished
	// operation; operations without committed deliveries stay pull-only.
	pendingTarget := uuid.NewString()
	pending := insertOperation(session, pendingTarget, "dispatching", "")
	quiet := insertOperation(session, uuid.NewString(), "dispatching", "")
	exec(`INSERT INTO computer_delivery_entries(team_id,operation_id,delivery_id,version,entry) VALUES((SELECT team_id FROM computer_operations WHERE id=$1),$1,$2,2,$3)`, pending, "deliv-1", `{"outcome":"committed"}`)
	fake := &fakeComputerObserver{}
	h.computerSource = fake
	if e = h.scanResults(ctx); e != nil {
		t.Fatal(e)
	}
	if len(fake.ids) != 1 || fake.ids[0] != pending || fake.ids[0] == quiet || fake.bindings[0].RunID == "" || fake.bindings[0].SessionID != session || fake.bindings[0].TargetID != pendingTarget {
		t.Fatalf("delivery commit pull: %+v %+v", fake.ids, fake.bindings)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != attentionCount {
		t.Fatalf("unfinished operation delivered: %d", len(received))
	}
}

type fakeComputerObserver struct {
	ids      []string
	bindings []toolcontext.ComputerBinding
}

func (f *fakeComputerObserver) Observe(_ context.Context, b toolcontext.ComputerBinding, id string) (computertask.Result, error) {
	f.ids = append(f.ids, id)
	f.bindings = append(f.bindings, b)
	return computertask.Result{}, nil
}

func (*fakeComputerObserver) ScanBatches(context.Context) error { return nil }
