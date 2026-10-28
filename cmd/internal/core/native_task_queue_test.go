package core

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/accounts"
	tools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/runtimefence"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Requires an explicitly supplied, migrated, isolated PostgreSQL database.
// Every row is synthetic and removed by its fresh fixture identity.
func TestNativeTaskQueuePostgresRestartAndDuplicateClaims(t *testing.T) {
	dsn := os.Getenv("MEMOH_NATIVE_QUEUE_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL DSN not supplied")
	}
	ctx := context.Background()
	open := func() *pgxpool.Pool {
		p, err := db.OpenPostgresDSN(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	pool := open()
	defer func() { pool.Close() }()
	user, bot, parent, child, source := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO users(id,username,is_active) VALUES($1,$2,true)`, user, "native-queue-"+user)
	defer func() {
		exec(`DELETE FROM bots WHERE id=$1`, bot)
		exec(`DELETE FROM team_members WHERE user_id=$1`, user)
		exec(`DELETE FROM users WHERE id=$1`, user)
	}()
	exec(`INSERT INTO team_members(user_id,role) VALUES($1,'member')`, user)
	exec(`INSERT INTO bots(id,owner_user_id,name) VALUES($1,$2,'native-queue-fixture')`, bot, user)
	exec(`INSERT INTO bot_sessions(id,bot_id,created_by_user_id,runtime_fencing_token) VALUES($1,$2,$3,1)`, parent, bot, user)
	exec(`INSERT INTO bot_sessions(id,bot_id,created_by_user_id,type,parent_session_id) VALUES($1,$2,$3,'subagent',$4)`, child, bot, user, parent)
	exec(`INSERT INTO session_runs(run_id,bot_id,session_id,invocation_id,turn_id,turn_position,state,input_json,input_fingerprint,fencing_token) VALUES($1,$2,$3,'source',$1,1,'running','{"query":"fixture"}','original-fingerprint',1)`, source, bot, parent)
	host := func() *nativeTaskQueueHost {
		store := postgresstore.NewWithPool(pool, dbsqlc.New(pool))
		a := accounts.NewService(nil, store)
		b := bots.NewService(nil, postgresstore.NewQueriesWithPool(pool, store.SQLC()))
		sessions := thread.NewService(nil, postgresstore.NewQueriesWithPool(pool, store.SQLC()), nil)
		h := &delegationHost{store: store, accounts: a, permissions: &applicationBotPermissionChecker{bots: b, accounts: a}, sessions: sessions}
		h.service = botdelegation.NewService(&botdelegation.Authority{Permissions: h, Sessions: sessions, Workdirs: delegationWorkdirs{store}, Mounts: store, Assets: h}, botdelegation.NewPostgresLedger(store.SQLC()), nil, nil, time.Minute)
		t.Cleanup(h.service.Close)
		h.service.Host = h
		return &nativeTaskQueueHost{host: h}
	}
	q := host()
	s := tools.SessionContext{BotID: bot, SessionID: parent, UserID: user, SessionType: "chat", SessionToken: "SECRET-NEVER-PERSIST"}
	fenced := runtimefence.WithContext(ctx, runtimefence.Fence{BotID: bot, SessionID: parent, Token: 1})
	enqueue := func(call string) dbstore.NativeTaskReceipt {
		t.Helper()
		r, created, err := q.Enqueue(fenced, s, call, "worker", child, thread.SubagentConfig{}, call, "Fixture "+call)
		if err != nil || !created {
			t.Fatalf("enqueue: %v %v", created, err)
		}
		return r
	}
	first, second := enqueue("first"), enqueue("second")
	retry, created, err := q.Enqueue(fenced, s, "first", "worker", child, thread.SubagentConfig{}, "first", "Fixture first")
	if err != nil || created || retry.TaskID != first.TaskID {
		t.Fatalf("enqueue retry: %v %v", created, err)
	}
	var raw []byte
	var fingerprint string
	if err = pool.QueryRow(ctx, `SELECT input_json,input_fingerprint FROM session_runs WHERE run_id=$1`, source).Scan(&raw, &fingerprint); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "SECRET") || fingerprint != "original-fingerprint" {
		t.Fatal("credentials or input fingerprint changed")
	}
	pool.Close()
	pool = open()
	q = host() // Fresh connections and host, with no in-memory task map.
	pending, err := q.Pending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, r := range pending {
		if r.BotID == bot {
			found = append(found, r.TaskID)
		}
	}
	if len(found) != 2 || found[0] != first.TaskID || found[1] != second.TaskID {
		t.Fatalf("restart identity/order: %v", found)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var winners []dbstore.NativeTaskReceipt
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, won, e := q.Claim(ctx, first)
			mu.Lock()
			defer mu.Unlock()
			if e != nil {
				t.Error(e)
			}
			if won {
				winners = append(winners, r)
			}
		}()
	}
	wg.Wait()
	if len(winners) != 1 {
		t.Fatalf("claims=%d", len(winners))
	}
	first = winners[0]
	if _, won, e := q.Claim(ctx, second); e != nil || won {
		t.Fatalf("successor bypassed unresolved claim: %v %v", won, e)
	}
	list := func() []dbstore.BotTaskDirectoryCandidate {
		rows, e := q.host.store.ListBotTaskDirectoryCandidates(ctx, dbstore.SessionTaskFilter{BotID: bot, UserID: user, SnapshotAt: time.Now(), Limit: 100})
		if e != nil {
			t.Fatal(e)
		}
		return rows
	}
	rows := list()
	if len(rows) != 2 {
		t.Fatalf("pre-execution directory=%d", len(rows))
	}
	states := map[string]string{}
	for _, r := range rows {
		states[r.ID] = r.State
	}
	if states[first.TaskID] != "unknown" || states[second.TaskID] != "queued" {
		t.Fatal(states)
	}
	run := uuid.NewString()
	input, _ := json.Marshal(map[string]string{"bot_id": bot, "thread_id": child, "agent_id": "worker"})
	exec(`INSERT INTO session_runs(run_id,bot_id,session_id,invocation_id,turn_id,turn_position,state,input_json,input_fingerprint) VALUES($1,$2,$3,$4,$1,1,'completed',$5,'execution')`, run, bot, child, "subagent:"+first.TaskID, input)
	// Completion committed before callback: startup observes the original run.
	pool.Close()
	pool = open()
	q = host()
	if _, err = q.Pending(ctx); err != nil {
		t.Fatal(err)
	}
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state, e := q.Finish(ctx, first, run, "completed")
			if e != nil || state != "completed" {
				t.Errorf("duplicate result: %s %v", state, e)
			}
		}()
	}
	wg.Wait()
	rows = list()
	if len(rows) != 2 {
		t.Fatalf("admission/execution duplicated: %d", len(rows))
	}
	for _, r := range rows {
		if r.ID == first.TaskID && (r.RunID != run || r.RecordID != first.ID || r.State != "completed") {
			t.Fatalf("execution association: %+v", r)
		}
	}
	second, created, err = q.Claim(ctx, second)
	if err != nil || !created {
		t.Fatalf("successor blocked after observed terminal: %v", err)
	}
	state, err := q.Finish(ctx, second, "", "completed")
	if err != nil || state != "unknown" {
		t.Fatalf("unproven success: %s %v", state, err)
	}
	if _, won, e := q.Claim(ctx, second); e != nil || won {
		t.Fatalf("unknown was redispatched: %v %v", won, e)
	}
	failed := enqueue("known-failure")
	if err = q.Reject(ctx, failed); err != nil {
		t.Fatal(err)
	}
	canceled := enqueue("canceled")
	if err = q.Cancel(canceled.TaskID); err != nil {
		t.Fatal(err)
	}
	revoked := enqueue("revoked")
	exec(`UPDATE users SET is_active=false WHERE id=$1`, user)
	if _, won, e := q.Claim(ctx, revoked); e == nil || won {
		t.Fatal("revoked account claimed work")
	}
	exec(`UPDATE users SET is_active=true WHERE id=$1`, user)
	states = map[string]string{}
	for _, r := range list() {
		states[r.ID] = r.State
	}
	if states[failed.TaskID] != "failed" || states[canceled.TaskID] != "aborted" || states[second.TaskID] != "unknown" {
		t.Fatal(states)
	}
	// Revoke while a claim is blocked at the existing Bot lock. Authorization
	// checked before waiting must not authorize execution after the wait.
	child = uuid.NewString()
	exec(`INSERT INTO bot_sessions(id,bot_id,created_by_user_id,type,parent_session_id) VALUES($1,$2,$3,'subagent',$4)`, child, bot, user, parent)
	late := enqueue("revoke-during-claim")
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT id FROM bots WHERE id=$1 FOR NO KEY UPDATE`, bot); err != nil {
		t.Fatal(err)
	}
	type claimResult struct {
		won bool
		err error
	}
	done := make(chan claimResult, 1)
	go func() { _, won, e := q.Claim(ctx, late); done <- claimResult{won, e} }()
	deadline := time.Now().Add(3 * time.Second)
	blocked := false
	for time.Now().Before(deadline) {
		if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%name: LockBotForSessionRunClaim%')`).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("claim never reached the controlled lock")
	}
	exec(`UPDATE users SET is_active=false WHERE id=$1`, user)
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.won || result.err == nil {
			t.Fatal("revocation during lock wait authorized execution")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("claim did not finish")
	}
	exec(`UPDATE users SET is_active=true WHERE id=$1`, user)
	for _, r := range list() {
		if r.ID == late.TaskID && r.State != "unknown" {
			t.Fatalf("uncertain claim was lost: %+v", r)
		}
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM session_runs WHERE bot_id=$1`, bot).Scan(&count); err != nil || count != 2 {
		t.Fatalf("fabricated executions count=%d %v", count, err)
	}
	t.Log("actual PostgreSQL: pre-admission identity, fresh-host restart, 8 concurrent claims/1 winner, 8 duplicate callbacks/1 execution, terminal recovery, unknown/no replay, failure/cancel/revocation including lock wait, directory dedupe, credential exclusion")
}
