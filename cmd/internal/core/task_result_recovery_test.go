package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/agent/application"
	"github.com/felinics/memoh/internal/agent/turn"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/taskresult"
)

func TestSavedResultRecoveryPostgres(t *testing.T) {
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
	p := open()
	defer func() { p.Close() }()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := p.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	user, bot, target, parent, child := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO users(id,username,is_active) VALUES($1,$2,true)`, user, "recovery-fixture-"+user)
	exec(`INSERT INTO team_members(user_id,role) VALUES($1,'member')`, user)
	for _, b := range []string{bot, target} {
		exec(`INSERT INTO bots(id,owner_user_id,name) VALUES($1,$2,$3)`, b, user, "recovery-fixture-"+b)
	}
	defer func() {
		exec(`DELETE FROM bots WHERE id IN ($1,$2)`, bot, target)
		exec(`DELETE FROM team_members WHERE user_id=$1`, user)
		exec(`DELETE FROM users WHERE id=$1`, user)
	}()
	exec(`INSERT INTO bot_sessions(id,bot_id,created_by_user_id,metadata) VALUES($1,$2,$3,'{"workspace_target_id":"native"}')`, parent, bot, user)
	exec(`INSERT INTO bot_sessions(id,bot_id,created_by_user_id,metadata) VALUES($1,$2,$3,'{"workspace_target_id":"native"}')`, child, target, user)
	var mu sync.Mutex
	var captured []turn.StartTurnCommand
	newHost := func() *delegationHost {
		store := postgresstore.NewWithPool(p, sqlc.New(p))
		a := accounts.NewService(nil, store)
		b := bots.NewService(nil, postgresstore.NewQueriesWithPool(p, store.SQLC()))
		sessions := thread.NewService(nil, postgresstore.NewQueriesWithPool(p, store.SQLC()), nil)
		h := &delegationHost{store: store, accounts: a, permissions: &applicationBotPermissionChecker{bots: b, accounts: a}, sessions: sessions}
		h.service = botdelegation.NewService(&botdelegation.Authority{Permissions: h, Sessions: sessions, Workdirs: delegationWorkdirs{store}, Mounts: store, Assets: h}, botdelegation.NewPostgresLedger(store.SQLC()), nil, nil, time.Minute)
		h.service.Host = h
		t.Cleanup(h.service.Close)
		h.resultSubmit = func(_ context.Context, _ string, c turn.StartTurnCommand) error {
			mu.Lock()
			defer mu.Unlock()
			captured = append(captured, c)
			return nil
		}
		return h
	}
	h := newHost()
	type sample struct{ id, event, worker, previous, child string }
	seed := func() sample {
		t.Helper()
		s := sample{id: uuid.NewString(), worker: uuid.NewString(), child: uuid.NewString()}
		exec(`INSERT INTO bot_sessions(id,bot_id,created_by_user_id,metadata) VALUES($1,$2,$3,'{"workspace_target_id":"native"}')`, s.child, target, user)
		exec(`INSERT INTO session_runs(run_id,bot_id,session_id,invocation_id,turn_id,turn_position,state,input_json,input_fingerprint) VALUES($1,$2,$3,$4,$1,1,'completed','{}','fixture')`, s.worker, target, s.child, "bot-delegation:"+s.id)
		exec(`INSERT INTO bot_delegations(id,user_id,source_bot_id,source_session_id,source_root_id,target_bot_id,target_session_id,request_id,run_id,fingerprint,source_selection,state,payload,result) VALUES($1,$2,$3,$4,$4,$5,$6,$1,$7,'recovery-fixture','native','completed','{"result_delivery_version":1,"target_selection":"native"}','{"summary":{"text":"Saved original outcome"}}')`, s.id, user, bot, parent, target, s.child, s.worker)
		task, e := h.loadResultTask(ctx, "bot_delegation", s.id, bot, user, parent)
		if e != nil {
			t.Fatal(e)
		}
		s.event = taskresult.EventID(task.state, task.run, task.text)
		for attempt := 1; attempt <= 3; attempt++ {
			run := uuid.NewString()
			ref := taskresult.Reference{Kind: "bot_delegation", TaskID: s.id, EventID: s.event, Attempt: attempt}
			exec(`INSERT INTO session_runs(run_id,bot_id,session_id,invocation_id,turn_id,turn_position,state,input_json,input_fingerprint) VALUES($1,$2,$3,$4,$1,1,'failed','{}','fixture')`, run, bot, parent, application.HostFollowUpInvocation(bot, parent, ref.String()))
			s.previous = run
		}
		raw, _ := json.Marshal(taskresult.Delivery{s.event: {ReportState: "attention", State: task.state, RunID: task.run, Text: task.text, Attempt: 3}})
		if e = task.save(ctx, task.raw, raw); e != nil {
			t.Fatal(e)
		}
		return s
	}
	read := func(s sample) taskresult.Event {
		t.Helper()
		task, e := h.loadResultTask(ctx, "bot_delegation", s.id, bot, user, parent)
		if e != nil {
			t.Fatal(e)
		}
		return task.delivery[s.event]
	}
	recover := func(s sample, attempt int) (botdelegation.RecoveryView, error) {
		return h.service.RecoverDelivery(ctx, bot, user, parent, s.id, botdelegation.RecoveryInput{EventID: s.event, ExpectedAttempt: attempt})
	}
	admit := func(s sample, attempt int, state string) string {
		run := uuid.NewString()
		ref := taskresult.Reference{Kind: "bot_delegation", TaskID: s.id, EventID: s.event, Attempt: attempt}
		exec(`INSERT INTO session_runs(run_id,bot_id,session_id,invocation_id,turn_id,turn_position,state,input_json,input_fingerprint) VALUES($1,$2,$3,$4,$1,1,$5,'{}','fixture')`, run, bot, parent, application.HostFollowUpInvocation(bot, parent, ref.String()), state)
		return run
	}
	publish := func(s sample, run string, attempt int) {
		ref := taskresult.Reference{Kind: "bot_delegation", TaskID: s.id, EventID: s.event, Attempt: attempt}
		meta, _ := json.Marshal(map[string]string{taskresult.MetadataKey: ref.String()})
		exec(`INSERT INTO bot_history_messages(id,bot_id,session_id,run_id,role,content,metadata,turn_visible) VALUES($1,$2,$3,$4,'assistant','"Saved report"',$5,true)`, uuid.NewString(), bot, parent, run, meta)
	}
	t.Run("concurrent_authorization_keeps_one_attempt_and_original_execution", func(t *testing.T) {
		s := seed()
		before := len(captured)
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if v, e := recover(s, 3); e != nil || v.Event.Attempt != 4 {
					t.Errorf("recover %+v %v", v, e)
				}
			}()
		}
		wg.Wait()
		e := read(s)
		if e.Attempt != 4 || len(e.Recoveries) != 1 || e.Recoveries[0].PreviousRunID != s.previous || e.Recoveries[0].RequestedBy != user {
			t.Fatal(e)
		}
		for _, c := range captured[before:] {
			ref, err := taskresult.Parse(c.TaskResultReceipt)
			if err != nil || ref.EventID != s.event || ref.Attempt != 4 || c.ThreadID != parent {
				t.Fatalf("changed identity: %+v %v", c, err)
			}
		}
		var executions, failed int
		_ = p.QueryRow(ctx, `SELECT count(*) FROM session_runs WHERE session_id=$1 AND invocation_id=$2`, s.child, "bot-delegation:"+s.id).Scan(&executions)
		_ = p.QueryRow(ctx, `SELECT count(*) FROM session_runs WHERE session_id=$1 AND state='failed'`, parent).Scan(&failed)
		if executions != 1 || failed < 3 {
			t.Fatalf("history/execution changed %d %d", executions, failed)
		}
	})
	t.Run("grant_before_enqueue_crash_and_uncertain_enqueue_resume_same_receipt", func(t *testing.T) {
		s := seed()
		submit := h.resultSubmit
		h.resultSubmit = func(context.Context, string, turn.StartTurnCommand) error {
			return errors.New("synthetic lost enqueue reply")
		}
		if _, e := recover(s, 3); e == nil {
			t.Fatal("lost reply hidden")
		}
		saved := read(s)
		if saved.Attempt != 4 {
			t.Fatal(saved)
		}
		h.resultSubmit = submit
		p.Close()
		p = open()
		h = newHost()
		v, e := recover(s, 3)
		if e != nil || v.Event.Attempt != 4 || len(v.Event.Recoveries) != 1 {
			t.Fatalf("restart minted another attempt %+v %v", v, e)
		}
	})
	t.Run("failed_recovery_does_not_automatically_expand_budget", func(t *testing.T) {
		s := seed()
		if _, e := recover(s, 3); e != nil {
			t.Fatal(e)
		}
		admit(s, 4, "failed")
		if _, e := recover(s, 3); e != nil {
			t.Fatal(e)
		}
		e := read(s)
		if e.Attempt != 4 || e.ReportState != "attention" {
			t.Fatal(e)
		}
		v, err := recover(s, 4)
		if err != nil || v.Event.Attempt != 5 || len(v.Event.Recoveries) != 2 {
			t.Fatalf("new explicit authorization %+v %v", v, err)
		}
		if _, err = recover(s, 3); !errors.Is(err, botdelegation.ErrConflict) {
			t.Fatal("stale recovery advanced budget", err)
		}
	})
	t.Run("published_receipt_wins_before_acknowledgement", func(t *testing.T) {
		s := seed()
		publish(s, s.previous, 3)
		if _, e := recover(s, 3); !errors.Is(e, botdelegation.ErrConflict) {
			t.Fatal(e)
		}
		if read(s).Attempt != 3 {
			t.Fatal("published result retried")
		}
	})
	t.Run("published_recovery_survives_reconnect_without_resubmission", func(t *testing.T) {
		s := seed()
		if _, e := recover(s, 3); e != nil {
			t.Fatal(e)
		}
		run := admit(s, 4, "lost")
		publish(s, run, 4)
		before := len(captured)
		p.Close()
		p = open()
		h = newHost()
		if _, e := recover(s, 3); !errors.Is(e, botdelegation.ErrConflict) {
			t.Fatal(e)
		}
		if e := h.reconcileResult(ctx, "bot_delegation", s.id, bot, user, parent); e != nil {
			t.Fatal(e)
		}
		if e := read(s); e.ReportState != "reported" || e.MessageID == "" || len(captured) != before {
			t.Fatal("published summary repeated", e)
		}
	})
	t.Run("active_or_missing_previous_admission_conflicts", func(t *testing.T) {
		for _, state := range []string{"running", "missing"} {
			s := seed()
			if state == "missing" {
				exec(`DELETE FROM session_runs WHERE run_id=$1`, s.previous)
			} else {
				exec(`UPDATE session_runs SET state=$2 WHERE run_id=$1`, s.previous, state)
			}
			if _, e := recover(s, 3); !errors.Is(e, botdelegation.ErrConflict) {
				t.Fatal(state, e)
			}
			if read(s).Attempt != 3 {
				t.Fatal("uncertain admission advanced")
			}
		}
	})
	t.Run("active_original_worker_cannot_be_recovered", func(t *testing.T) {
		s := seed()
		exec(`UPDATE session_runs SET state='running' WHERE run_id=$1`, s.worker)
		if _, e := recover(s, 3); !errors.Is(e, botdelegation.ErrConflict) {
			t.Fatal(e)
		}
	})
	t.Run("wrong_event_attempt_owner_or_source_conflicts", func(t *testing.T) {
		s := seed()
		wrong := s
		wrong.event = taskresult.EventID("wrong", "", "")
		if _, e := recover(wrong, 3); !errors.Is(e, botdelegation.ErrConflict) {
			t.Fatal(e)
		}
		if _, e := recover(s, 4); !errors.Is(e, botdelegation.ErrConflict) {
			t.Fatal(e)
		}
		for _, args := range [][3]string{{bot, uuid.NewString(), parent}, {target, user, parent}, {bot, user, child}} {
			if _, e := h.service.RecoverDelivery(ctx, args[0], args[1], args[2], s.id, botdelegation.RecoveryInput{EventID: s.event, ExpectedAttempt: 3}); !errors.Is(e, botdelegation.ErrDenied) {
				t.Fatal(args, e)
			}
		}
	})
	t.Run("revocation_and_changed_recipient_fail_before_authorization", func(t *testing.T) {
		s := seed()
		exec(`UPDATE users SET is_active=false WHERE id=$1`, user)
		_, err := recover(s, 3)
		exec(`UPDATE users SET is_active=true WHERE id=$1`, user)
		if !errors.Is(err, botdelegation.ErrDenied) {
			t.Fatal(err)
		}
		exec(`UPDATE bot_sessions SET channel_type='telegram' WHERE id=$1`, parent)
		_, err = recover(s, 3)
		exec(`UPDATE bot_sessions SET channel_type='' WHERE id=$1`, parent)
		if !errors.Is(err, botdelegation.ErrDenied) {
			t.Fatal(err)
		}
		if read(s).Attempt != 3 {
			t.Fatal("unauthorized grant persisted")
		}
	})
	t.Run("revocation_after_grant_blocks_queued_admission", func(t *testing.T) {
		s := seed()
		if _, e := recover(s, 3); e != nil {
			t.Fatal(e)
		}
		c := captured[len(captured)-1]
		exec(`UPDATE users SET is_active=false WHERE id=$1`, user)
		_, _, err := h.prepareResult(ctx, application.ChatRequest{BotID: bot, UserID: user, ThreadID: parent, TaskResultReceipt: c.TaskResultReceipt, DelegationResultID: s.id, CurrentChannel: c.CurrentChannel, ChatID: c.ChatID, ReplyTarget: c.ReplyTarget})
		exec(`UPDATE users SET is_active=true WHERE id=$1`, user)
		if !errors.Is(err, botdelegation.ErrDenied) {
			t.Fatal(err)
		}
	})
	t.Run("legacy_unknown_and_ungranted_attempt_are_not_opted_in", func(t *testing.T) {
		for _, state := range []string{"unknown", "legacy"} {
			s := seed()
			if state == "unknown" {
				exec(`UPDATE bot_delegations SET state='unknown' WHERE id=$1`, s.id)
			} else {
				exec(`UPDATE bot_delegations SET payload='{}' WHERE id=$1`, s.id)
			}
			if _, e := recover(s, 3); !errors.Is(e, botdelegation.ErrConflict) {
				t.Fatal(state, e)
			}
		}
		s := seed()
		task, err := h.loadResultTask(ctx, "bot_delegation", s.id, bot, user, parent)
		if err != nil {
			t.Fatal(err)
		}
		e := task.delivery[s.event]
		e.Attempt = 4
		task.delivery[s.event] = e
		raw, _ := json.Marshal(task.delivery)
		if err = task.save(ctx, task.raw, raw); err != nil {
			t.Fatal(err)
		}
		if err = h.reconcileResult(ctx, "bot_delegation", s.id, bot, user, parent); !errors.Is(err, botdelegation.ErrDenied) {
			t.Fatal("ungranted higher attempt executed", err)
		}
	})
}
