-- Image workflows (M4, migration 0191). One workflow is configuration: a name,
-- the natural-language instruction, the two minted pairing labels and one
-- lifecycle state. One run is a batch snapshot. Nothing here stores an album
-- uri or album name; the phone keeps those.

-- name: CreateImageWorkflow :one
INSERT INTO public.image_workflows (user_id, bot_id, name, instruction, input_pair_key, output_pair_key, state)
VALUES (sqlc.arg(user_id), sqlc.arg(bot_id), sqlc.arg(name), sqlc.arg(instruction),
        sqlc.arg(input_pair_key), sqlc.arg(output_pair_key), 'active')
RETURNING *;

-- name: ListImageWorkflows :many
SELECT * FROM public.image_workflows
WHERE team_id = public.memoh_current_team_id() AND user_id = sqlc.arg(user_id)
ORDER BY created_at, id;

-- name: GetImageWorkflow :one
SELECT * FROM public.image_workflows
WHERE team_id = public.memoh_current_team_id() AND user_id = sqlc.arg(user_id)
  AND id = sqlc.arg(id);

-- name: GetImageWorkflowByName :one
SELECT * FROM public.image_workflows
WHERE team_id = public.memoh_current_team_id() AND user_id = sqlc.arg(user_id)
  AND name = sqlc.arg(name);

-- Owner-scoped only by team: the delivery sink attributes a product from a run,
-- which already belongs to one owner.
-- name: GetImageWorkflowByID :one
SELECT * FROM public.image_workflows
WHERE team_id = public.memoh_current_team_id() AND id = sqlc.arg(id);

-- name: UpdateImageWorkflow :one
UPDATE public.image_workflows
SET name = sqlc.arg(name), instruction = sqlc.arg(instruction), updated_at = now()
WHERE team_id = public.memoh_current_team_id() AND user_id = sqlc.arg(user_id)
  AND id = sqlc.arg(id)
RETURNING *;

-- Repeating a state keeps the first timestamp: pausing a paused workflow is one
-- write, not a churn.
-- name: SetImageWorkflowState :one
UPDATE public.image_workflows
SET state = sqlc.arg(state),
    updated_at = CASE WHEN state = sqlc.arg(state) THEN updated_at ELSE now() END
WHERE team_id = public.memoh_current_team_id() AND user_id = sqlc.arg(user_id)
  AND id = sqlc.arg(id)
RETURNING *;

-- name: ListLiveImageWorkflowEdges :many
-- The pairing graph: every workflow that is not revoked is one directed edge.
SELECT id, input_pair_key, output_pair_key FROM public.image_workflows
WHERE team_id = public.memoh_current_team_id() AND user_id = sqlc.arg(user_id)
  AND state <> 'revoked'
ORDER BY created_at, id;

-- name: CreateImageWorkflowRun :one
INSERT INTO public.image_workflow_runs (team_id, workflow_id, schedule_id, session_id, instruction_snapshot, files, fingerprint, state)
SELECT w.team_id, w.id, sqlc.narg(schedule_id), sqlc.narg(session_id), sqlc.arg(instruction_snapshot), sqlc.arg(files)::jsonb, sqlc.arg(fingerprint), sqlc.arg(state)
FROM public.image_workflows w
WHERE w.team_id = public.memoh_current_team_id() AND w.id = sqlc.arg(workflow_id)
RETURNING team_id, id, workflow_id, schedule_id, session_id, instruction_snapshot, files, fingerprint, state, result, error, created_at, updated_at;

-- name: GetImageWorkflowRunByFingerprint :one
SELECT * FROM public.image_workflow_runs
WHERE team_id = public.memoh_current_team_id() AND workflow_id = sqlc.arg(workflow_id)
  AND fingerprint = sqlc.arg(fingerprint);

-- name: SetImageWorkflowRunSchedule :one
UPDATE public.image_workflow_runs
SET schedule_id = sqlc.arg(schedule_id), state = sqlc.arg(state), updated_at = now()
WHERE team_id = public.memoh_current_team_id() AND id = sqlc.arg(id)
RETURNING *;

-- name: SetImageWorkflowRunState :one
UPDATE public.image_workflow_runs
SET state = sqlc.arg(state), updated_at = now()
WHERE team_id = public.memoh_current_team_id() AND id = sqlc.arg(id)
RETURNING *;

-- name: ListImageWorkflowRuns :many
SELECT * FROM public.image_workflow_runs
WHERE team_id = public.memoh_current_team_id() AND workflow_id = sqlc.arg(workflow_id)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(row_limit);

-- name: ListHeldImageWorkflowRuns :many
SELECT * FROM public.image_workflow_runs
WHERE team_id = public.memoh_current_team_id() AND workflow_id = sqlc.arg(workflow_id)
  AND state = 'held'
ORDER BY created_at, id;

-- name: CountImageWorkflowRunsByState :many
SELECT state, count(*)::bigint AS run_count FROM public.image_workflow_runs
WHERE team_id = public.memoh_current_team_id() AND workflow_id = sqlc.arg(workflow_id)
GROUP BY state;

-- Product attribution: the run whose execution session is this one, so a
-- committed upload can name the workflow that produced it.
-- name: GetImageWorkflowRunBySession :one
SELECT * FROM public.image_workflow_runs
WHERE team_id = public.memoh_current_team_id() AND session_id = sqlc.arg(session_id)
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- The lineage walk's one step: a session's parent.
-- name: GetSessionParentSessionID :one
SELECT parent_session_id FROM bot_sessions
WHERE team_id = public.memoh_current_team_id() AND id = sqlc.arg(id);

-- name: GetBareAlbumPairKey :one
SELECT pair_key FROM public.album_pairs
WHERE team_id = public.memoh_current_team_id() AND user_id = sqlc.arg(user_id)
  AND bot_id = sqlc.arg(bot_id) AND state = 'active' AND workflow_id IS NULL;
