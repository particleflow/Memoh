-- name: ListBotTaskDirectoryCandidates :many
-- Discovery from original durable task admissions/receipts. A session, a
-- configured Agent or an ordinary conversation run is never a task row.
-- record_id disambiguates native background IDs reused after a process restart.
WITH candidates AS (
  SELECT 'native_agent'::text AS task_type,
    substring(r.invocation_id FROM 10)::text AS task_id,
    r.run_id AS record_id, r.session_id, r.created_at, r.updated_at,
    r.state::text AS state, s.title::text AS name, r.run_id::text AS run_id,
    r.bot_id AS source_bot_id, s.parent_session_id AS source_session_id,
    NULL::uuid AS schedule_id, s.created_by_user_id AS owner_user_id, false AS queue_admission
  FROM session_runs r
  JOIN bot_sessions s ON s.team_id=r.team_id AND s.bot_id=r.bot_id AND s.id=r.session_id
  WHERE r.team_id=public.memoh_current_team_id() AND r.bot_id=sqlc.arg(bot_id)::uuid
    AND s.type='subagent' AND s.parent_session_id IS NOT NULL
    AND r.invocation_id LIKE 'subagent:%' AND length(r.invocation_id)>9
    AND r.input_json->>'bot_id'=r.bot_id::text
    AND r.input_json->>'thread_id'=r.session_id::text
    AND COALESCE(r.input_json->>'agent_id','')<>''
    AND NOT EXISTS (SELECT 1 FROM session_runs source CROSS JOIN LATERAL
      jsonb_each(COALESCE(source.input_json->'native_task_queue','{}'::jsonb)) AS task(task_key,task_data)
      WHERE source.team_id=r.team_id AND source.bot_id=r.bot_id
      AND task.task_data->>'session_id'=r.session_id::text
      AND 'subagent:'||(task.task_data->>'task_id')=r.invocation_id)
  UNION ALL
  SELECT 'native_agent',task.task_data->>'task_id',(task.task_data->>'id')::uuid,
    child.id,(task.task_data->>'created_at')::timestamptz,
    COALESCE(execution.updated_at,(task.task_data->>'created_at')::timestamptz),
    COALESCE(execution.state,CASE WHEN task.task_data->>'state'='claimed' THEN 'unknown' ELSE task.task_data->>'state' END),
    task.task_data->>'description',COALESCE(execution.run_id::text,''),
    source.bot_id,source.session_id,NULL::uuid,(task.task_data->>'user_id')::uuid,true
  FROM session_runs source CROSS JOIN LATERAL
    jsonb_each(COALESCE(source.input_json->'native_task_queue','{}'::jsonb)) AS task(task_key,task_data)
  JOIN bot_sessions child ON child.team_id=source.team_id AND child.bot_id=source.bot_id
    AND child.id::text=task.task_data->>'session_id' AND child.parent_session_id=source.session_id
    AND child.type='subagent'
  LEFT JOIN session_runs execution ON execution.team_id=source.team_id AND execution.bot_id=source.bot_id
    AND execution.session_id=child.id AND execution.invocation_id='subagent:'||(task.task_data->>'task_id')
  WHERE source.team_id=public.memoh_current_team_id() AND source.bot_id=sqlc.arg(bot_id)::uuid
    AND task.task_data->>'version'='1' AND task.task_data->>'source_run_id'=source.run_id::text
    AND task.task_data->>'bot_id'=source.bot_id::text AND task.task_data->>'parent_session_id'=source.session_id::text
    AND task.task_key=task.task_data->>'id' AND child.created_by_user_id::text=task.task_data->>'user_id'
  UNION ALL
  SELECT 'computer_operation', op.id::text, op.id, op.session_id,
    op.created_at, op.updated_at, op.state, s.title::text, COALESCE(op.run_id,''),
    op.bot_id, op.session_id, NULL::uuid, op.user_id, false
  FROM computer_operations op
  JOIN bot_sessions s ON s.team_id=op.team_id AND s.bot_id=op.bot_id AND s.id=op.session_id
  WHERE op.team_id=public.memoh_current_team_id() AND op.bot_id=sqlc.arg(bot_id)::uuid
    AND op.user_id=sqlc.arg(user_id)::uuid
  UNION ALL
  SELECT 'bot_delegation', d.id::text, d.id, d.target_session_id,
    d.created_at, d.updated_at, d.state, COALESCE(target.title,''), d.run_id::text,
    d.source_bot_id, d.source_session_id, NULL::uuid, d.user_id, false
  FROM bot_delegations d
  LEFT JOIN bot_sessions target ON target.team_id=d.team_id AND target.bot_id=d.target_bot_id AND target.id=d.target_session_id
  WHERE d.team_id=public.memoh_current_team_id() AND d.user_id=sqlc.arg(user_id)::uuid
    AND (d.source_bot_id=sqlc.arg(bot_id)::uuid OR d.target_bot_id=sqlc.arg(bot_id)::uuid)
  UNION ALL
  SELECT 'schedule_fire', l.id::text, l.id, l.session_id, l.started_at,
    COALESCE(l.completed_at,l.started_at),
    CASE WHEN l.completed_at IS NULL THEN COALESCE(r.state,'unknown')
      WHEN l.status='error' THEN 'failed' WHEN l.status='ok' THEN 'completed' ELSE 'unknown' END,
    scheduled.name::text, COALESCE(r.run_id::text,''), l.bot_id,
    l.session_id, l.schedule_id, NULL::uuid, false
  FROM schedule_logs l
  JOIN schedule scheduled ON scheduled.team_id=l.team_id AND scheduled.bot_id=l.bot_id AND scheduled.id=l.schedule_id
  LEFT JOIN session_runs r ON r.team_id=l.team_id AND r.bot_id=l.bot_id AND r.session_id=l.session_id
    AND r.invocation_id='schedule:'||l.schedule_id::text||':'||l.id::text
  WHERE l.team_id=public.memoh_current_team_id() AND l.bot_id=sqlc.arg(bot_id)::uuid
)
SELECT * FROM candidates
WHERE created_at<=sqlc.arg(snapshot_at)::timestamptz
  AND (NOT sqlc.arg(use_task)::boolean OR
    (task_type=sqlc.arg(task_type)::text AND record_id=sqlc.arg(record_id)::uuid))
  AND (NOT sqlc.arg(use_cursor)::boolean OR
    (created_at,task_type COLLATE "C",record_id)<(sqlc.arg(before_time)::timestamptz,sqlc.arg(before_type)::text COLLATE "C",sqlc.arg(before_id)::uuid))
ORDER BY created_at DESC,task_type COLLATE "C" DESC,record_id DESC
LIMIT sqlc.arg(page_size)::integer;
