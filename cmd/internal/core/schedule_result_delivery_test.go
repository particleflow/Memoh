package core

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	sdk "github.com/felinics/twilight/sdk"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/agent/application"
	tools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/agent/toolexec"
	"github.com/felinics/memoh/internal/agent/turn"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/channel/route"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/inputsource"
	"github.com/felinics/memoh/internal/runtimefence"
	"github.com/felinics/memoh/internal/schedule"
	"github.com/felinics/memoh/internal/taskresult"
)

type scheduleToolCapture struct {
	tools.Scheduler
	calls int
}

func (c *scheduleToolCapture) Create(_ context.Context, _ string, req schedule.CreateRequest) (schedule.Schedule, error) {
	c.calls++
	return schedule.Schedule{Name: req.Name}, nil
}

// Schedule-specific PostgreSQL fixtures. No model, timer, or external action.
// Captured summaries are persisted through the existing fenced message writer.
func TestScheduleResultPostgresCrashWindows(t *testing.T) {
	dsn := os.Getenv("MEMOH_SCHEDULE_RESULT_TEST_DSN")
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
	exec(`INSERT INTO bot_sessions(id,bot_id,created_by_user_id,type,parent_session_id) VALUES($1,$2,$3,'schedule',NULL)`, child, bot, user)
	exec(`INSERT INTO session_runs(run_id,bot_id,session_id,invocation_id,turn_id,turn_position,state,input_json,input_fingerprint,fencing_token) VALUES($1,$2,$3,'source',$1,1,'running','{}','source',1)`, source, bot, parent)
	var received []turn.StartTurnCommand
	var mu sync.Mutex
	host := func() *delegationHost {
		store := postgresstore.NewWithPool(pool, sqlc.New(pool))
		a := accounts.NewService(nil, store)
		b := bots.NewService(nil, postgresstore.NewQueriesWithPool(pool, store.SQLC()))
		sessions := thread.NewService(nil, postgresstore.NewQueriesWithPool(pool, store.SQLC()), nil)
		h := &delegationHost{store: store, accounts: a, permissions: &applicationBotPermissionChecker{bots: b, accounts: a}, sessions: sessions, messages: messagepkg.NewService(nil, postgresstore.NewQueriesWithPool(pool, store.SQLC())), routes: route.NewService(nil, postgresstore.NewQueriesWithPool(pool, store.SQLC()))}
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
	routeID := uuid.NewString()
	exec(`INSERT INTO bot_channel_routes(id,bot_id,channel_type,external_conversation_id,conversation_type,default_reply_target,active_session_id) VALUES($1,$2,'web',$3,'private',$2::uuid::text,$4)`, routeID, bot, "contact:"+user, parent)
	exec(`UPDATE bot_sessions SET route_id=$1,channel_type='local',runtime_type='model',type='chat' WHERE id=$2`, routeID, parent)
	b, e := h.service.Authority.Source(ctx, user, bot, parent)
	if e != nil {
		t.Fatal(e)
	}
	routing, e := h.SourceRoute(ctx, b)
	if e != nil {
		t.Fatal(e)
	}
	origin, e := json.Marshal(scheduleResultOrigin{Version: 1, Identity: b, Route: routing})
	if e != nil {
		t.Fatal(e)
	}
	if got, e := h.AuthorizeScheduleResultOrigin(ctx, bot, origin); e != nil || got != user {
		t.Fatal(got, e)
	}

	capture := &scheduleToolCapture{}
	adapter := &scheduleContactProvider{base: tools.NewScheduleProvider(nil, capture), host: h}
	definitions, e := adapter.Tools(ctx, tools.SessionContext{UserID: user, BotID: bot, SessionID: parent, SessionType: "chat", CurrentPlatform: "web"})
	if e != nil {
		t.Fatal(e)
	}
	var create toolexec.ToolExecuteFunc
	for _, definition := range definitions {
		if definition.Name == tools.ToolCreateSchedule().String() {
			create = definition.Execute
		}
	}
	if create == nil {
		t.Fatal("create adapter absent")
	}
	call := &toolexec.ToolExecContext{Context: ctx}
	args := sdk.ParseToolArguments(`{"name":"tool fixture","description":"isolated","pattern":"0 0 1 1 *","command":"no model or timer","enabled":false}`)
	if _, e = create(call, args); e != nil || capture.calls != 1 {
		t.Fatal("valid original contact tool rejected", e)
	}
	exec(`UPDATE bot_channel_routes SET active_session_id=NULL WHERE id=$1`, routeID)
	if _, e = create(call, args); e == nil || capture.calls != 1 {
		t.Fatal("stale tool descriptor bypassed current contact authority")
	}
	exec(`UPDATE bot_channel_routes SET active_session_id=$1 WHERE id=$2`, parent, routeID)
	plan, fire, worker := uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO schedule(id,bot_id,name,description,pattern,command,enabled,result_origin) VALUES($1,$2,'synthetic','fixture','0 0 1 1 *','no external execution',false,$3)`, plan, bot, origin)
	log, err := h.store.CreateScheduleLog(ctx, sqlc.CreateScheduleLogParams{ScheduleID: nativeTaskUUID(plan), BotID: nativeTaskUUID(bot), SessionID: nativeTaskUUID(child), ResultOrigin: origin})
	if err != nil {
		t.Fatal(err)
	}
	fire = log.ID.String()
	exec(`INSERT INTO session_runs(run_id,bot_id,session_id,invocation_id,turn_id,turn_position,state,input_json,input_fingerprint) VALUES($1,$2,$3,$4,$1,1,'completed','{}','schedule-fixture')`, worker, bot, child, "schedule:"+plan+":"+fire)
	exec(`INSERT INTO bot_history_messages(bot_id,session_id,role,content,run_id,turn_id,turn_position) VALUES($1,$2,'assistant','"Original scheduled result"',$3,$3,1)`, bot, child, worker)
	beforeCallback, e := h.loadScheduleResult(ctx, fire, bot, user, parent)
	if e != nil {
		t.Fatal(e)
	}
	if _, err = h.store.CompleteScheduleLog(ctx, sqlc.CompleteScheduleLogParams{ID: log.ID, Status: "ok", ResultText: "Original scheduled result"}); err != nil {
		t.Fatal(err)
	}
	afterCallback, e := h.loadScheduleResult(ctx, fire, bot, user, parent)
	if e != nil || beforeCallback.text != afterCallback.text || beforeCallback.state != afterCallback.state {
		t.Fatal("late callback changed result event", e)
	}
	// C1: result saved, no summary, recreated process-owned state and connections.
	pool.Close()
	pool = open()
	h = host()
	reconcile := func() {
		t.Helper()
		if e := h.reconcileResult(ctx, "schedule_fire", fire, bot, user, parent); e != nil {
			t.Fatal(e)
		}
	}
	if e := h.scanScheduleResults(ctx); e != nil {
		t.Fatal(e)
	}
	reconcile()
	if len(received) != 2 || received[0].TaskResultReceipt != received[1].TaskResultReceipt {
		t.Fatal("schedule C1 changed receipt")
	}
	first := received[0]
	ref, err := taskresult.Parse(first.TaskResultReceipt)
	if err != nil || ref.Kind != "schedule_fire" || ref.TaskID != fire {
		t.Fatal(ref, err)
	}
	if first.ThreadID != parent || first.UserID != user {
		t.Fatal("wrong scheduled return identity")
	}
	fenced := runtimefence.WithContext(ctx, runtimefence.Fence{BotID: bot, SessionID: parent, Token: 1})
	exec(`UPDATE session_runs SET state='completed' WHERE run_id=$1`, source)
	position := int64(2)
	admit := func(cmd turn.StartTurnCommand) string {
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
			t.Fatal(e)
		}
		return messages[0].ID
	}
	// C2: progress then failure, followed by empty final, neither counts as report.
	run := admit(first)
	write(run, "Preparing scheduled report", "")
	exec(`UPDATE session_runs SET state='failed' WHERE run_id=$1`, run)
	position++
	reconcile()
	second := received[len(received)-1]
	ref, _ = taskresult.Parse(second.TaskResultReceipt)
	if ref.Attempt != 2 {
		t.Fatal("schedule progress consumed", ref)
	}
	run = admit(second)
	exec(`UPDATE session_runs SET state='completed' WHERE run_id=$1`, run)
	position++
	reconcile()
	third := received[len(received)-1]
	ref, _ = taskresult.Parse(third.TaskResultReceipt)
	if ref.Attempt != 3 {
		t.Fatal("schedule empty consumed", ref)
	}
	run = admit(third)
	messageID := write(run, "Here is the saved scheduled result", third.TaskResultReceipt)
	exec(`UPDATE session_runs SET state='lost' WHERE run_id=$1`, run)
	// C3: summary committed but consumption ACK lost; eight concurrent recoveries.
	before := len(received)
	pool.Close()
	pool = open()
	h = host()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := h.reconcileResult(ctx, "schedule_fire", fire, bot, user, parent); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if len(received) != before {
		t.Fatal("schedule summary repeated")
	}
	task, e := h.loadResultTask(ctx, "schedule_fire", fire, bot, user, parent)
	if e != nil || task.delivery[ref.EventID].MessageID != messageID {
		t.Fatal("lost publication", e)
	}
	// Plan soft-deletion does not delete or change the already original fire.
	if e = h.store.DeleteSchedule(ctx, nativeTaskUUID(plan)); e != nil {
		t.Fatal(e)
	}
	reconcile()
	if len(received) != before {
		t.Fatal("deleted schedule replayed")
	}
	// Legacy schedules/logs are never enrolled; run association cannot use latest.
	legacy := uuid.NewString()
	exec(`INSERT INTO schedule_logs(id,schedule_id,bot_id,session_id,status,completed_at) VALUES($1,$2,$3,$4,'ok',now())`, legacy, plan, bot, child)
	if _, e = h.loadResultTask(ctx, "schedule_fire", legacy, bot, user, parent); e == nil {
		t.Fatal("legacy opted in")
	}
	unknown := uuid.NewString()
	exec(`INSERT INTO schedule_logs(id,schedule_id,bot_id,session_id,result_origin) VALUES($1,$2,$3,$4,$5)`, unknown, plan, bot, child, origin)
	lost := uuid.NewString()
	exec(`INSERT INTO session_runs(run_id,bot_id,session_id,invocation_id,turn_id,turn_position,state,input_json,input_fingerprint) VALUES($1,$2,$3,$4,$1,2,'lost','{}','lost')`, lost, bot, child, "schedule:"+plan+":"+unknown)
	if e = h.reconcileResult(ctx, "schedule_fire", unknown, bot, user, parent); e != nil {
		t.Fatal(e)
	}
	task, e = h.loadResultTask(ctx, "schedule_fire", unknown, bot, user, parent)
	if e != nil || task.state != "unknown" || task.run != lost {
		t.Fatal("unknown lost or latest guessed", e)
	}
	exec(`UPDATE schedule_logs SET status='error',completed_at=now() WHERE id=$1`, unknown)
	task, e = h.loadResultTask(ctx, "schedule_fire", unknown, bot, user, parent)
	if e != nil || task.state != "unknown" {
		t.Fatal("late callback concealed unknown", e)
	}
	failed := uuid.NewString()
	exec(`INSERT INTO schedule_logs(id,schedule_id,bot_id,result_origin,status,error_code,completed_at) VALUES($1,$2,$3,$4,'error','synthetic',now())`, failed, plan, bot, origin)
	task, e = h.loadResultTask(ctx, "schedule_fire", failed, bot, user, parent)
	if e != nil || task.state != "failed" {
		t.Fatal("failure without run lost", e)
	}
	// Permission/contact changes block both execution authorization and publication preparation.
	cmd := received[len(received)-1]
	req := application.ChatRequest{TaskResultReceipt: cmd.TaskResultReceipt, BotID: bot, ThreadID: parent, UserID: user, CurrentChannel: cmd.CurrentChannel, ChatID: cmd.ChatID, RouteID: cmd.RouteID, ReplyTarget: cmd.ReplyTarget, ConversationType: cmd.ConversationType}
	_, preparedSource, sourceErr := h.prepareResult(ctx, req)
	if sourceErr != nil || preparedSource.InputSource == nil || preparedSource.InputSource.Kind != inputsource.Result || preparedSource.InputSource.TaskID != plan || preparedSource.InputSource.OriginBotID != bot || preparedSource.InputSource.UserID != "" {
		t.Fatalf("schedule result source: %+v %v", preparedSource.InputSource, sourceErr)
	}
	if _, _, e = h.prepareResult(ctx, req); e != nil {
		t.Fatal("valid schedule result rejected", e)
	}
	exec(`UPDATE bot_channel_routes SET active_session_id=NULL WHERE id=$1`, routeID)
	if _, _, e = h.prepareResult(ctx, req); e == nil {
		t.Fatal("changed contact accepted")
	}
	exec(`UPDATE bot_channel_routes SET active_session_id=$1 WHERE id=$2`, parent, routeID)
	exec(`UPDATE users SET is_active=false WHERE id=$1`, user)
	if _, e = h.AuthorizeScheduleResultOrigin(ctx, bot, origin); e == nil {
		t.Fatal("revoked schedule executes")
	}
	if _, _, e = h.prepareResult(ctx, req); e == nil {
		t.Fatal("revoked schedule publishes")
	}
	var count int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM session_runs WHERE session_id=$1`, child).Scan(&count); e != nil || count != 2 {
		t.Fatal("schedule action replayed", count, e)
	}
	t.Log("schedule-specific C1/C2/C3 passed; 8 concurrent publication recoveries; failed/unknown/legacy preserved; deleted plan retained; exact fire/run; contact change and revocation refused; zero model/external actions")
}
