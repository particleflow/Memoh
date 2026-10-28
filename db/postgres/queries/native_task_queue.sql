-- Native tool queue admissions live beside their original source turn input.
-- They do not occupy a child runtime slot and are never fabricated run rows.
-- Existing resume/decision writers patch other input_json keys independently.
-- name: GetNativeTaskSource :one
SELECT r.* FROM session_runs r
JOIN bot_sessions s ON s.team_id=r.team_id AND s.id=r.session_id AND s.bot_id=r.bot_id
WHERE r.team_id=public.memoh_current_team_id()
 AND r.bot_id=sqlc.arg(bot_id)::uuid AND r.session_id=sqlc.arg(session_id)::uuid
 AND r.fencing_token=sqlc.arg(fencing_token)::bigint
 AND s.created_by_user_id=sqlc.arg(user_id)::uuid AND s.deleted_at IS NULL
ORDER BY r.created_at DESC LIMIT 1;

-- name: EnqueueNativeTask :one
UPDATE session_runs r
SET input_json=jsonb_set(r.input_json,'{native_task_queue}',
 COALESCE(r.input_json->'native_task_queue','{}'::jsonb)||
 jsonb_build_object(sqlc.arg(task_key)::text,sqlc.arg(task_data)::jsonb || jsonb_build_object('created_at',clock_timestamp())))
WHERE r.team_id=public.memoh_current_team_id() AND r.run_id=sqlc.arg(source_run_id)::uuid
 AND r.bot_id=sqlc.arg(bot_id)::uuid AND r.session_id=sqlc.arg(parent_session_id)::uuid
 AND r.state IN ('running','waiting_decision') AND r.abort_requested_at IS NULL
 AND r.fencing_token=sqlc.arg(fencing_token)::bigint
 AND NOT (COALESCE(r.input_json->'native_task_queue','{}'::jsonb) ? sqlc.arg(task_key)::text)
 AND EXISTS (SELECT 1 FROM bot_sessions parent JOIN bot_sessions child
   ON child.team_id=parent.team_id AND child.bot_id=parent.bot_id AND child.parent_session_id=parent.id
   WHERE parent.team_id=r.team_id AND parent.id=r.session_id AND parent.deleted_at IS NULL
   AND parent.runtime_fencing_token=r.fencing_token
   AND parent.created_by_user_id=sqlc.arg(user_id)::uuid
   AND child.id=sqlc.arg(child_session_id)::uuid AND child.type='subagent'
   AND child.deleted_at IS NULL AND child.created_by_user_id=parent.created_by_user_id)
RETURNING (r.input_json->'native_task_queue'->sqlc.arg(task_key)::text)::jsonb AS task_data;

-- name: ReadNativeTask :one
SELECT (r.input_json->'native_task_queue'->sqlc.arg(task_key)::text)::jsonb AS task_data
FROM session_runs r WHERE r.team_id=public.memoh_current_team_id()
 AND r.run_id=sqlc.arg(source_run_id)::uuid AND r.bot_id=sqlc.arg(bot_id)::uuid
 AND r.input_json->'native_task_queue' ? sqlc.arg(task_key)::text;

-- name: ClaimNativeTask :one
UPDATE session_runs r
SET input_json=jsonb_set(r.input_json,ARRAY['native_task_queue',sqlc.arg(task_key)::text],
 (r.input_json->'native_task_queue'->sqlc.arg(task_key)::text)||
 jsonb_build_object('state','claimed','claim_token',sqlc.arg(claim_token)::text))
WHERE r.team_id=public.memoh_current_team_id() AND r.run_id=sqlc.arg(source_run_id)::uuid
 AND r.bot_id=sqlc.arg(bot_id)::uuid AND r.abort_requested_at IS NULL
 AND r.input_json->'native_task_queue'->sqlc.arg(task_key)::text->>'state'='queued'
 AND NOT EXISTS (SELECT 1 FROM session_runs execution WHERE execution.team_id=r.team_id
   AND execution.session_id=sqlc.arg(child_session_id)::uuid
   AND execution.state IN ('accepted','running','waiting_decision','finishing'))
 AND NOT EXISTS (SELECT 1 FROM session_runs source CROSS JOIN LATERAL
   jsonb_each(COALESCE(source.input_json->'native_task_queue','{}'::jsonb)) pending
   WHERE source.team_id=r.team_id AND source.bot_id=r.bot_id
   AND pending.value->>'session_id'=sqlc.arg(child_session_id)::text
   AND pending.value->>'state' IN ('claimed','unknown'))
RETURNING (r.input_json->'native_task_queue'->sqlc.arg(task_key)::text)::jsonb AS task_data;

-- name: FinishNativeTask :one
UPDATE session_runs r
SET input_json=jsonb_set(r.input_json,ARRAY['native_task_queue',sqlc.arg(task_key)::text],
 (r.input_json->'native_task_queue'->sqlc.arg(task_key)::text)||
 jsonb_build_object('state',sqlc.arg(task_state)::text,'run_id',sqlc.arg(run_id)::text))
WHERE r.team_id=public.memoh_current_team_id() AND r.run_id=sqlc.arg(source_run_id)::uuid
 AND r.bot_id=sqlc.arg(bot_id)::uuid
 AND r.input_json->'native_task_queue'->sqlc.arg(task_key)::text->>'state' IN ('claimed','unknown')
 AND r.input_json->'native_task_queue'->sqlc.arg(task_key)::text->>'claim_token'=sqlc.arg(claim_token)::text
RETURNING (r.input_json->'native_task_queue'->sqlc.arg(task_key)::text)::jsonb AS task_data;

-- name: CancelQueuedNativeTask :one
UPDATE session_runs r
SET input_json=jsonb_set(r.input_json,ARRAY['native_task_queue',sqlc.arg(task_key)::text,'state'],'"aborted"'::jsonb)
WHERE r.team_id=public.memoh_current_team_id() AND r.run_id=sqlc.arg(source_run_id)::uuid
 AND r.bot_id=sqlc.arg(bot_id)::uuid
 AND r.input_json->'native_task_queue'->sqlc.arg(task_key)::text->>'state'='queued'
RETURNING (r.input_json->'native_task_queue'->sqlc.arg(task_key)::text)::jsonb AS task_data;

-- name: ListQueuedNativeTasks :many
SELECT r.run_id AS source_run_id,r.bot_id,entry.task_key::text AS task_key,entry.task_data::jsonb AS task_data
FROM session_runs r CROSS JOIN LATERAL jsonb_each(COALESCE(r.input_json->'native_task_queue','{}'::jsonb)) AS entry(task_key, task_data)
WHERE r.team_id=public.memoh_current_team_id() AND entry.task_data->>'state'='queued'
ORDER BY (entry.task_data->>'created_at')::timestamptz,r.run_id,entry.task_key;

-- name: RejectQueuedNativeTask :one
UPDATE session_runs r
SET input_json=jsonb_set(r.input_json,ARRAY['native_task_queue',sqlc.arg(task_key)::text,'state'],'"failed"'::jsonb)
WHERE r.team_id=public.memoh_current_team_id() AND r.run_id=sqlc.arg(source_run_id)::uuid
 AND r.bot_id=sqlc.arg(bot_id)::uuid
 AND r.input_json->'native_task_queue'->sqlc.arg(task_key)::text->>'state'='queued'
RETURNING (r.input_json->'native_task_queue'->sqlc.arg(task_key)::text)::jsonb AS task_data;

-- name: FindNativeTask :one
SELECT entry.task_data::jsonb AS task_data FROM session_runs r CROSS JOIN LATERAL
 jsonb_each(COALESCE(r.input_json->'native_task_queue','{}'::jsonb)) AS entry(task_key, task_data)
WHERE r.team_id=public.memoh_current_team_id()
 AND entry.task_data->>'task_id'=sqlc.arg(task_id)::text;

-- name: ListClaimedNativeTasks :many
SELECT entry.task_data::jsonb AS task_data
FROM session_runs source CROSS JOIN LATERAL
 jsonb_each(COALESCE(source.input_json->'native_task_queue','{}'::jsonb)) AS entry(task_key,task_data)
JOIN session_runs execution ON execution.team_id=source.team_id AND execution.bot_id=source.bot_id
 AND execution.session_id::text=entry.task_data->>'session_id'
 AND execution.invocation_id='subagent:'||(entry.task_data->>'task_id')
WHERE source.team_id=public.memoh_current_team_id()
 AND entry.task_data->>'state' IN ('claimed','unknown')
 AND execution.state IN ('completed','failed','aborted','lost');
