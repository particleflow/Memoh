package core

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/agent/turn"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/channel/route"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/taskresult"
)

func TestScheduleOccurrenceUnknownReturnsOriginalContact(t *testing.T) {
	dsn := os.Getenv("MEMOH_SCHEDULE_OCCURRENCE_TEST_DSN")
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

	plan, fire := uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO schedule(id,bot_id,name,description,pattern,command,enabled,result_origin) VALUES($1,$2,'no-run fixture','isolated','0 0 1 1 *','no external execution',false,$3)`, plan, bot, origin)
	exec(`INSERT INTO schedule_logs(id,schedule_id,bot_id,result_origin,scheduled_for,claim_state,claim_deadline) VALUES($1,$2,$3,$4,now(),'claimed',now()+interval '1 hour')`, fire, plan, bot, origin)
	if e = h.scanScheduleResults(ctx); e != nil || len(received) != 0 {
		t.Fatal("live pre-admission claim reported", e)
	}
	exec(`UPDATE schedule_logs SET claim_deadline=clock_timestamp()-interval '1 second' WHERE id=$1`, fire)
	pool.Close()
	pool = open()
	h = host()
	if e = h.scanScheduleResults(ctx); e != nil || len(received) != 1 {
		t.Fatal("no-run recovery not delivered", e)
	}
	cmd := received[0]
	ref, e := taskresult.Parse(cmd.TaskResultReceipt)
	if e != nil || ref.Kind != "schedule_fire" || ref.TaskID != fire || cmd.ThreadID != parent || cmd.UserID != user {
		t.Fatal("wrong unknown origin", e)
	}
	task, e := h.loadResultTask(ctx, "schedule_fire", fire, bot, user, parent)
	if e != nil || task.state != "unknown" || task.run != "" {
		t.Fatal("unknown pretended to execute", e)
	}
	exec(`UPDATE schedule_logs SET completed_at=now(),status='error',error_code='late_timeout' WHERE id=$1`, fire)
	task, e = h.loadResultTask(ctx, "schedule_fire", fire, bot, user, parent)
	if e != nil || task.state != "unknown" {
		t.Fatal("late callback concealed pre-admission uncertainty", e)
	}
	exec(`UPDATE users SET is_active=false WHERE id=$1`, user)
	if e = h.scanScheduleResults(ctx); e != nil {
		t.Fatal(e)
	}
	if len(received) != 1 {
		t.Fatal("revoked user got another return")
	}
	var count int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM session_runs WHERE bot_id=$1 AND invocation_id LIKE 'schedule:%'`, bot).Scan(&count); e != nil || count != 0 {
		t.Fatal("recovery created execution", count, e)
	}
	t.Log("expired no-run fire returned unknown to original contact after host/pool reconstruction; active claim stayed pending; revoked user blocked; zero execution admissions")
}
