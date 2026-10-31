//go:build integration

package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/computertask"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/mcp"
	"github.com/felinics/memoh/internal/toolcontext"
)

type profileHistoryPermission struct {
	bot, user string
	denied    bool
}

func (p *profileHistoryPermission) HasBotPermission(_ context.Context, bot, user, permission string) (bool, error) {
	return !p.denied && bot == p.bot && user == p.user && permission == "workspace_exec", nil
}

func TestNativeProfileCutoverPersistedHistoryScope(t *testing.T) {
	f := newNativeHerdrFixture(t)
	profile := agenvo.NativeHerdrProfile{InstanceID: "synthetic-native-instance", Session: "synthetic", WorkDir: `C:\Synthetic`, BashPath: `C:\Git\bin\bash.exe`, HerdrCLI: `C:\Synthetic\herdr.exe`, HerdrConfigRoot: `C:\Synthetic\herdr`, Agent: agenvo.AgentProfile{ExecutionMode: "pi-json-v1", Provider: "synthetic", Model: "synthetic", ConfigDir: `C:\Synthetic\old`}}
	old, _ := json.Marshal(map[string]any{"herdr": profile})
	f.exec(t, `UPDATE user_runtimes SET provider_config=$2 WHERE id=$1`, f.runtime, old)
	f.config = old
	target, err := agenvo.DecodeNativeTarget(f.runtime, old)
	if err != nil {
		t.Fatal(err)
	}
	input := f.operation(f.mount)
	input.TargetFingerprint = agenvo.TargetFingerprint(target)
	input.Details, _ = json.Marshal(computertask.Details{Kind: "herdr_pi_task", TargetSnapshot: &target, Phase: "batch_submitted", NativeEvidence: &computertask.NativeEvidenceLedger{Version: 1, Observations: []computertask.NativeObservationRecord{{ID: "saved-observation", Origin: "native_read"}}}})
	op, won, err := f.store.ClaimComputerOperation(t.Context(), input)
	if err != nil || !won {
		t.Fatal(won, err)
	}
	op.State = "unknown"
	op, err = f.store.SaveComputerOperation(t.Context(), op)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(op)
	permission := &profileHistoryPermission{bot: f.bot, user: f.owner}
	calls := 0
	relay := agenvo.NewService(f.store, permission, func(context.Context, agenvo.Scope, agenvo.Target, string, map[string]any) (agenvo.Receipt, error) {
		calls++
		return agenvo.Receipt{}, errors.New("unexpected native call")
	})
	source := computertask.NewSource(computertask.NewService(f.store, f.store, relay, nil, nil))
	binding := toolcontext.ComputerBinding{BotID: f.bot, UserID: f.owner, SessionID: f.session, TargetID: f.mount, RunID: op.RunID}
	ctx, err := toolcontext.WithComputerBinding(t.Context(), binding)
	if err != nil {
		t.Fatal(err)
	}
	session := mcp.ToolSessionContext{BotID: f.bot, UserID: f.owner, SessionID: f.session, RunID: op.RunID, RunContext: ctx}
	args := map[string]any{"operation_id": op.ID}
	original, err := source.CallTool(ctx, session, "computer_task_get", args)
	if err != nil {
		t.Fatal(err)
	}
	historyArgs := map[string]any{"operation_id": op.ID, "history_offset": 0}
	savedReceipt, err := source.CallTool(ctx, session, "computer_task_get", historyArgs)
	if err != nil {
		t.Fatal(err)
	}
	request := uuid.NewString()
	gate := f.change(t, request, 0, true)
	profile.Agent.ConfigDir = `C:\Synthetic\new`
	next, _ := json.Marshal(map[string]any{"herdr": profile})
	if _, err = f.store.CutoverPausedNativeProfile(t.Context(), f.owner, f.runtime, f.mount, old, next, gate.InstanceID, request, gate.Revision); err != nil {
		t.Fatal(err)
	}
	got, err := source.CallTool(ctx, session, "computer_task_get", args)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(original)
	b, _ := json.Marshal(got)
	if string(a) != string(b) {
		t.Fatal("historical response changed after cutover")
	}
	receiptAfter, err := source.CallTool(ctx, session, "computer_task_get", historyArgs)
	if err != nil {
		t.Fatal(err)
	}
	receiptBeforeBytes, _ := json.Marshal(savedReceipt)
	receiptAfterBytes, _ := json.Marshal(receiptAfter)
	if string(receiptBeforeBytes) != string(receiptAfterBytes) {
		t.Fatal("historical receipt metadata changed")
	}
	for _, tool := range []string{"computer_task_result", "computer_task_cancel", "computer_task_logs", "computer_task_artifact"} {
		if _, err = source.CallTool(ctx, session, tool, args); err == nil {
			t.Fatalf("old execution/file pin accepted: %s", tool)
		}
	}
	if _, err = source.Recover(ctx, binding, op.ID); err == nil {
		t.Fatal("old live recovery accepted")
	}
	for _, field := range []string{"user", "bot", "session"} {
		bad := binding
		switch field {
		case "user":
			bad.UserID = uuid.NewString()
		case "bot":
			bad.BotID = uuid.NewString()
		case "session":
			bad.SessionID = uuid.NewString()
		}
		badctx, e := toolcontext.WithComputerBinding(t.Context(), bad)
		if e != nil {
			t.Fatal(e)
		}
		badSession := mcp.ToolSessionContext{BotID: bad.BotID, UserID: bad.UserID, SessionID: bad.SessionID, RunID: bad.RunID, RunContext: badctx}
		if _, e = source.CallTool(badctx, badSession, "computer_task_get", args); e == nil {
			t.Fatalf("foreign %s read", field)
		}
	}
	permission.denied = true
	if _, err = source.CallTool(ctx, session, "computer_task_get", args); err == nil {
		t.Fatal("revoked permission read")
	}
	permission.denied = false
	foreign := rlsConn(t, f.pool, teamMigrationDSN(t))
	if _, err = foreign.Exec(t.Context(), `SELECT set_config('memoh.team_id',$1,false)`, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	fs := postgresstore.NewWithQueries(dbsqlc.New(foreign))
	foreignSource := computertask.NewSource(computertask.NewService(fs, fs, agenvo.NewService(fs, permission, nil), nil, nil))
	if _, err = foreignSource.CallTool(ctx, session, "computer_task_get", args); err == nil {
		t.Fatal("foreign team read")
	}
	after, err := f.store.ReadComputerOperation(t.Context(), op)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(after)
	if string(before) != string(raw) || calls != 0 {
		t.Fatal("history changed operation or invoked native transport")
	}
	f.exec(t, `UPDATE user_runtimes SET revoked_at=now() WHERE id=$1`, f.runtime)
	checkRetiredHistory := func() {
		t.Helper()
		for _, input := range []map[string]any{args, historyArgs} {
			got, readErr := source.CallTool(ctx, session, "computer_task_get", input)
			if readErr != nil {
				t.Fatal(readErr)
			}
			gotBytes, _ := json.Marshal(got)
			expected := a
			if _, page := input["history_offset"]; page {
				expected = receiptBeforeBytes
			}
			if string(gotBytes) != string(expected) {
				t.Fatal("retirement changed saved history")
			}
		}
		for _, tool := range []string{"computer_task_result", "computer_task_cancel", "computer_task_logs", "computer_task_artifact"} {
			if _, readErr := source.CallTool(ctx, session, tool, args); readErr == nil {
				t.Fatalf("retired live path accepted: %s", tool)
			}
		}
		saved, readErr := f.store.ReadComputerOperation(ctx, op)
		savedBytes, _ := json.Marshal(saved)
		if readErr != nil || string(before) != string(savedBytes) || calls != 0 {
			t.Fatal("retirement changed ledger or invoked transport")
		}
	}
	checkRetiredHistory()
	f.exec(t, `DELETE FROM bot_remote_runtime_bindings WHERE id=$1`, f.mount)
	checkRetiredHistory()
	permission.denied = true
	if _, err = source.CallTool(ctx, session, "computer_task_get", args); err == nil {
		t.Fatal("retired history bypassed Bot permission")
	}
	permission.denied = false
	if _, err = foreignSource.CallTool(ctx, session, "computer_task_get", args); err == nil {
		t.Fatal("retired history crossed team")
	}
}
