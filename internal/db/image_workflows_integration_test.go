//go:build integration

package db_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felinics/memoh/internal/team"
)

// These tests drive the real schema: migration 0191 and the 0190 role columns.
// Every assertion below is a database-level contract fact
// (contracts/image-workflows.json, tables.*); the service layer may not weaken
// them.

func TestF1ImageWorkflowTablesExistWithTheirContractColumns(t *testing.T) {
	pool := freshMigratedDB(t)
	ctx := context.Background()

	for _, table := range []string{"image_workflows", "image_workflow_runs"} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+table).Scan(&exists); err != nil {
			t.Fatalf("inspect %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("table %s does not exist; migration 0191 is missing", table)
		}
	}

	columns := map[string]string{
		"image_workflows":     "team_id,id,user_id,bot_id,name,instruction,input_pair_key,output_pair_key,state,created_at,updated_at",
		"image_workflow_runs": "team_id,id,workflow_id,schedule_id,session_id,instruction_snapshot,files,fingerprint,state,result,error,created_at,updated_at",
	}
	for table, want := range columns {
		var got string
		if err := pool.QueryRow(ctx, `
			SELECT string_agg(column_name, ',' ORDER BY ordinal_position)
			  FROM information_schema.columns
			 WHERE table_schema='public' AND table_name=$1`, table).Scan(&got); err != nil {
			t.Fatalf("read %s columns: %v", table, err)
		}
		if got != want {
			t.Fatalf("%s columns = %q, want %q", table, got, want)
		}
	}
}

func TestF1ImageWorkflowTablesForceRowLevelSecurity(t *testing.T) {
	pool := freshMigratedDB(t)
	ctx := context.Background()
	for _, table := range []string{"image_workflows", "image_workflow_runs"} {
		var enabled, forced bool
		if err := pool.QueryRow(ctx, `
			SELECT relrowsecurity, relforcerowsecurity
			  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			 WHERE n.nspname='public' AND c.relname=$1`, table).Scan(&enabled, &forced); err != nil {
			t.Fatalf("inspect %s RLS: %v", table, err)
		}
		if !enabled || !forced {
			t.Fatalf("%s RLS enabled=%v forced=%v, want both true", table, enabled, forced)
		}
		var policies int
		if err := pool.QueryRow(ctx, `
			SELECT count(*) FROM pg_policies WHERE schemaname='public' AND tablename=$1`, table).Scan(&policies); err != nil {
			t.Fatalf("count %s policies: %v", table, err)
		}
		if policies != 4 {
			t.Fatalf("%s policies = %d, want 4 (select/insert/update/delete)", table, policies)
		}
	}
}

func TestF1ImageWorkflowSchemaEnforcesTheLoopPreventionConstraints(t *testing.T) {
	pool := freshMigratedDB(t)
	ctx := context.Background()
	owner, bot := uuid.NewString(), uuid.NewString()
	seedOwnerAndBot(t, pool, owner, bot)

	// 0a: a workflow never reads its own output.
	_, err := pool.Exec(ctx, `
		INSERT INTO image_workflows(user_id,bot_id,name,instruction,input_pair_key,output_pair_key)
		VALUES($1,$2,'self','x','same-label','same-label')`, owner, bot)
	if !isConstraint(t, err, "image_workflows_ports_isolated") {
		t.Fatalf("self-referencing insert error = %v, want image_workflows_ports_isolated", err)
	}

	// 0b: one output label belongs to at most one live workflow.
	if _, err := pool.Exec(ctx, `
		INSERT INTO image_workflows(user_id,bot_id,name,instruction,input_pair_key,output_pair_key)
		VALUES($1,$2,'first','x','in-1','out-1')`, owner, bot); err != nil {
		t.Fatalf("insert first workflow: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO image_workflows(user_id,bot_id,name,instruction,input_pair_key,output_pair_key)
		VALUES($1,$2,'second','x','in-2','out-1')`, owner, bot)
	if !isConstraint(t, err, "image_workflows_output_pair") {
		t.Fatalf("duplicate output label error = %v, want image_workflows_output_pair", err)
	}

	// A shared input label is allowed: two workflows may watch one album.
	if _, err := pool.Exec(ctx, `
		INSERT INTO image_workflows(user_id,bot_id,name,instruction,input_pair_key,output_pair_key)
		VALUES($1,$2,'third','x','in-1','out-3')`, owner, bot); err != nil {
		t.Fatalf("shared input label rejected: %v", err)
	}

	// 0d: the same batch identity never starts two runs of one workflow.
	var workflowID string
	if err := pool.QueryRow(ctx, `SELECT id FROM image_workflows WHERE user_id=$1 AND name='first'`, owner).Scan(&workflowID); err != nil {
		t.Fatalf("read workflow id: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO image_workflow_runs(team_id, workflow_id, instruction_snapshot, fingerprint)
		SELECT team_id, id, instruction, 'fp-1' FROM image_workflows WHERE id=$1`, workflowID); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO image_workflow_runs(team_id, workflow_id, instruction_snapshot, fingerprint)
		SELECT team_id, id, instruction, 'fp-1' FROM image_workflows WHERE id=$1`, workflowID)
	if !isConstraint(t, err, "image_workflow_runs_fingerprint") {
		t.Fatalf("duplicate fingerprint error = %v, want image_workflow_runs_fingerprint", err)
	}
}

func TestF1ImageWorkflowRunStatesAreTheContractFive(t *testing.T) {
	pool := freshMigratedDB(t)
	ctx := context.Background()
	owner, bot := uuid.NewString(), uuid.NewString()
	seedOwnerAndBot(t, pool, owner, bot)
	var workflowID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO image_workflows(user_id,bot_id,name,instruction,input_pair_key,output_pair_key)
		VALUES($1,$2,'states','x','in','out') RETURNING id`, owner, bot).Scan(&workflowID); err != nil {
		t.Fatalf("insert workflow: %v", err)
	}
	for state := range map[string]bool{"held": true, "armed": true, "running": true, "completed": true, "failed": true} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO image_workflow_runs(team_id, workflow_id, instruction_snapshot, fingerprint, state)
			SELECT team_id, id, instruction, $2, $3 FROM image_workflows WHERE id=$1`,
			workflowID, "fp-"+state, state); err != nil {
			t.Fatalf("run state %q rejected: %v", state, err)
		}
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO image_workflow_runs(team_id, workflow_id, instruction_snapshot, fingerprint, state)
		SELECT team_id, id, instruction, 'fp-bad', 'queued' FROM image_workflows WHERE id=$1`, workflowID); err == nil {
		t.Fatal("run state 'queued' was accepted; the contract has five states")
	}
}

func TestF2AlbumPairsCarryWorkflowRoleAndKeepTheBareTargetRule(t *testing.T) {
	pool := freshMigratedDB(t)
	ctx := context.Background()
	owner, bot := uuid.NewString(), uuid.NewString()
	seedOwnerAndBot(t, pool, owner, bot)

	var columns string
	if err := pool.QueryRow(ctx, `
		SELECT string_agg(column_name, ',' ORDER BY ordinal_position)
		  FROM information_schema.columns
		 WHERE table_schema='public' AND table_name='album_pairs'`).Scan(&columns); err != nil {
		t.Fatalf("read album_pairs columns: %v", err)
	}
	for _, want := range []string{"workflow_id", "role"} {
		if !containsColumn(columns, want) {
			t.Fatalf("album_pairs columns = %q, missing %q", columns, want)
		}
	}

	// A bare target keeps today's meaning: one active row per (user, bot).
	if _, err := pool.Exec(ctx, `
		INSERT INTO album_pairs(user_id,bot_id,pair_key,state) VALUES($1,$2,'bare-1','active')`, owner, bot); err != nil {
		t.Fatalf("insert bare pair: %v", err)
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO album_pairs(user_id,bot_id,pair_key,state) VALUES($1,$2,'bare-2','active')`, owner, bot)
	if !isConstraint(t, err, "album_pairs_active_bot") {
		t.Fatalf("second bare target error = %v, want album_pairs_active_bot", err)
	}

	// A workflow row carries a role, and the role vocabulary is closed.
	var workflowID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO image_workflows(user_id,bot_id,name,instruction,input_pair_key,output_pair_key)
		VALUES($1,$2,'wf','x','in','out') RETURNING id`, owner, bot).Scan(&workflowID); err != nil {
		t.Fatalf("insert workflow: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO album_pairs(user_id,bot_id,pair_key,state,workflow_id,role)
		VALUES($1,$2,'wf-in','active',$3,'input')`, owner, bot, workflowID); err != nil {
		t.Fatalf("insert workflow input pair: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO album_pairs(user_id,bot_id,pair_key,state,workflow_id,role)
		VALUES($1,$2,'wf-bad','active',$3,'sideways')`, owner, bot, workflowID)
	if !isConstraint(t, err, "album_pairs_role_check") {
		t.Fatalf("unknown role error = %v, want album_pairs_role_check", err)
	}
	// A role without a workflow is refused: the columns move together.
	if _, err := pool.Exec(ctx, `
		INSERT INTO album_pairs(user_id,bot_id,pair_key,state,role)
		VALUES($1,$2,'role-only','active','input')`, owner, bot); err == nil {
		t.Fatal("a role without a workflow id was accepted")
	}
}

func TestF2WorkflowPairIsUniquePerRoleAndDoesNotConsumeTheBareTarget(t *testing.T) {
	pool := freshMigratedDB(t)
	ctx := context.Background()
	owner, bot := uuid.NewString(), uuid.NewString()
	seedOwnerAndBot(t, pool, owner, bot)

	var first, second string
	if err := pool.QueryRow(ctx, `
		INSERT INTO image_workflows(user_id,bot_id,name,instruction,input_pair_key,output_pair_key)
		VALUES($1,$2,'one','x','in-1','out-1') RETURNING id`, owner, bot).Scan(&first); err != nil {
		t.Fatalf("insert first workflow: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO image_workflows(user_id,bot_id,name,instruction,input_pair_key,output_pair_key)
		VALUES($1,$2,'two','x','in-2','out-2') RETURNING id`, owner, bot).Scan(&second); err != nil {
		t.Fatalf("insert second workflow: %v", err)
	}
	for _, role := range []string{"input", "output"} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO album_pairs(user_id,bot_id,pair_key,state,workflow_id,role)
			VALUES($1,$2,$3,'active',$4,$5)`, owner, bot, first+"-"+role, first, role); err != nil {
			t.Fatalf("insert %s pair: %v", role, err)
		}
	}
	// A second workflow holds its own input and output rows on the same bot.
	for _, role := range []string{"input", "output"} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO album_pairs(user_id,bot_id,pair_key,state,workflow_id,role)
			VALUES($1,$2,$3,'active',$4,$5)`, owner, bot, second+"-"+role, second, role); err != nil {
			t.Fatalf("insert second workflow %s pair: %v", role, err)
		}
	}
	// The same role twice for one workflow is a conflict.
	_, err := pool.Exec(ctx, `
		INSERT INTO album_pairs(user_id,bot_id,pair_key,state,workflow_id,role)
		VALUES($1,$2,'extra','active',$3,'input')`, owner, bot, first)
	if !isConstraint(t, err, "album_pairs_active_workflow") {
		t.Fatalf("duplicate workflow role error = %v, want album_pairs_active_workflow", err)
	}
	// Workflow rows do not consume the bare single-target slot.
	if _, err := pool.Exec(ctx, `
		INSERT INTO album_pairs(user_id,bot_id,pair_key,state) VALUES($1,$2,'bare','active')`, owner, bot); err != nil {
		t.Fatalf("bare target alongside workflow pairs rejected: %v", err)
	}
}

func TestF2RetireQueryLeavesOtherWorkflowsAlone(t *testing.T) {
	pool := freshMigratedDB(t)
	ctx := context.Background()
	owner, bot := uuid.NewString(), uuid.NewString()
	seedOwnerAndBot(t, pool, owner, bot)

	var first, second string
	if err := pool.QueryRow(ctx, `
		INSERT INTO image_workflows(user_id,bot_id,name,instruction,input_pair_key,output_pair_key)
		VALUES($1,$2,'one','x','in-1','out-1') RETURNING id`, owner, bot).Scan(&first); err != nil {
		t.Fatalf("insert first workflow: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO image_workflows(user_id,bot_id,name,instruction,input_pair_key,output_pair_key)
		VALUES($1,$2,'two','x','in-2','out-2') RETURNING id`, owner, bot).Scan(&second); err != nil {
		t.Fatalf("insert second workflow: %v", err)
	}
	for _, workflow := range []string{first, second} {
		for _, role := range []string{"input", "output"} {
			if _, err := pool.Exec(ctx, `
				INSERT INTO album_pairs(user_id,bot_id,pair_key,state,workflow_id,role)
				VALUES($1,$2,$3,'active',$4,$5)`, owner, bot, workflow+"-"+role, workflow, role); err != nil {
				t.Fatalf("insert pair: %v", err)
			}
		}
	}
	// The rewritten retire query is scoped by workflow_id: designating a new
	// label for one workflow must not revoke another workflow's rows.
	if _, err := pool.Exec(ctx, `
		UPDATE album_pairs
		   SET state='revoked'
		 WHERE team_id = public.memoh_current_team_id()
		   AND user_id=$1 AND bot_id=$2 AND state='active'
		   AND pair_key <> 'new-' || $3
		   AND workflow_id IS NOT DISTINCT FROM $3::uuid`, owner, bot, first); err != nil {
		t.Fatalf("scoped retire: %v", err)
	}
	var otherActive int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM album_pairs
		 WHERE user_id=$1 AND workflow_id=$2 AND state='active'`, owner, second).Scan(&otherActive); err != nil {
		t.Fatalf("count second workflow pairs: %v", err)
	}
	if otherActive != 2 {
		t.Fatalf("second workflow active pairs = %d, want 2: a scoped retire must not touch it", otherActive)
	}
	var firstActive int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM album_pairs
		 WHERE user_id=$1 AND workflow_id=$2 AND state='active'`, owner, first).Scan(&firstActive); err != nil {
		t.Fatalf("count first workflow pairs: %v", err)
	}
	if firstActive != 0 {
		t.Fatalf("first workflow active pairs = %d, want 0 after its own retire", firstActive)
	}
}

func TestF2UnscopedRetireStillRetiresOnlyBareTargets(t *testing.T) {
	pool := freshMigratedDB(t)
	ctx := context.Background()
	owner, bot := uuid.NewString(), uuid.NewString()
	seedOwnerAndBot(t, pool, owner, bot)

	if _, err := pool.Exec(ctx, `
		INSERT INTO album_pairs(user_id,bot_id,pair_key,state) VALUES($1,$2,'old-bare','active')`, owner, bot); err != nil {
		t.Fatalf("insert bare pair: %v", err)
	}
	var workflowID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO image_workflows(user_id,bot_id,name,instruction,input_pair_key,output_pair_key)
		VALUES($1,$2,'wf','x','in','out') RETURNING id`, owner, bot).Scan(&workflowID); err != nil {
		t.Fatalf("insert workflow: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO album_pairs(user_id,bot_id,pair_key,state,workflow_id,role)
		VALUES($1,$2,'wf-out','active',$3,'output')`, owner, bot, workflowID); err != nil {
		t.Fatalf("insert workflow output pair: %v", err)
	}
	// The bare path passes NULL: only bare rows are retired.
	if _, err := pool.Exec(ctx, `
		UPDATE album_pairs SET state='revoked'
		 WHERE team_id = public.memoh_current_team_id()
		   AND user_id=$1 AND bot_id=$2 AND state='active'
		   AND pair_key <> 'new-bare'
		   AND workflow_id IS NOT DISTINCT FROM NULL::uuid`, owner, bot); err != nil {
		t.Fatalf("bare retire: %v", err)
	}
	var workflowActive int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM album_pairs WHERE user_id=$1 AND workflow_id=$2 AND state='active'`,
		owner, workflowID).Scan(&workflowActive); err != nil {
		t.Fatalf("count workflow pairs: %v", err)
	}
	if workflowActive != 1 {
		t.Fatalf("workflow output pair active = %d, want 1: a bare designation must not retire it", workflowActive)
	}
	var bareState string
	if err := pool.QueryRow(ctx, `
		SELECT state FROM album_pairs WHERE user_id=$1 AND pair_key='old-bare'`, owner).Scan(&bareState); err != nil {
		t.Fatalf("read bare pair state: %v", err)
	}
	if bareState != "revoked" {
		t.Fatalf("bare pair state = %q, want revoked", bareState)
	}
}

func containsColumn(columns, want string) bool {
	for _, part := range strings.Split(columns, ",") {
		if part == want {
			return true
		}
	}
	return false
}

// isConstraint reports whether err is the named constraint violation.
func isConstraint(t *testing.T, err error, name string) bool {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	switch pgErr.Code {
	case "23514", "23505": // check_violation, unique_violation
		return pgErr.ConstraintName == name
	default:
		return false
	}
}

// seedOwnerAndBot inserts one owner and one Bot, the two rows every workflow
// row references.
func seedOwnerAndBot(t *testing.T, pool *pgxpool.Pool, owner, bot string) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "SELECT set_config('memoh.team_id',$1,false)", team.DefaultTeamID); err != nil {
		t.Fatalf("select team: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO users(id,username) VALUES($1,'image-workflow-test')", owner); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO team_members(user_id) VALUES($1)", owner); err != nil {
		t.Fatalf("insert membership: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO bots(id,owner_user_id,name) VALUES($1,$2,'image-workflow-bot')", bot, owner); err != nil {
		t.Fatalf("insert bot: %v", err)
	}
}
