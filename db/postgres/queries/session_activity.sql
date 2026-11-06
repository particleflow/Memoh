-- name: ListSessionActivityCandidates :many
-- Discovery only. The service rechecks ordinary session/ancestor access and
-- the original operation target before returning a name or navigation ID.
WITH candidates AS (
  SELECT 'session_run'::text AS task_type, r.run_id AS task_id, r.session_id, r.created_at,
    COALESCE(NULLIF(agent.name,''),s.title)::text AS name
  FROM session_runs r
  JOIN bot_sessions s ON s.team_id=r.team_id AND s.id=r.session_id
  LEFT JOIN bot_agents agent ON agent.team_id=s.team_id AND agent.bot_id=s.bot_id AND agent.id=s.bot_agent_id
  WHERE r.team_id=public.memoh_current_team_id() AND r.bot_id=sqlc.arg(bot_id)::uuid
    AND s.bot_id=r.bot_id AND s.deleted_at IS NULL
    AND r.state IN ('accepted','running','waiting_decision','finishing')
    AND r.error_code IS DISTINCT FROM 'history_reset'
    AND (r.fencing_token=0 OR s.runtime_fencing_token<=r.fencing_token)
    AND NOT EXISTS (SELECT 1 FROM session_runs newer
      WHERE newer.team_id=r.team_id AND newer.session_id=r.session_id
        AND (newer.created_at,newer.run_id)>(r.created_at,r.run_id))
  UNION ALL
  SELECT 'computer_operation'::text, op.id, op.session_id, op.created_at, s.title::text
  FROM computer_operations op
  JOIN bot_sessions s ON s.team_id=op.team_id AND s.id=op.session_id
  WHERE op.team_id=public.memoh_current_team_id() AND op.bot_id=sqlc.arg(bot_id)::uuid
    AND op.user_id=sqlc.arg(user_id)::uuid AND s.bot_id=op.bot_id AND s.deleted_at IS NULL
    AND op.state='submitted'
)
SELECT task_type,task_id,session_id,created_at,name FROM candidates
WHERE created_at<=sqlc.arg(snapshot_at)::timestamptz
  AND (NOT sqlc.arg(use_cursor)::boolean OR
    (created_at,task_type,task_id)<(sqlc.arg(before_time)::timestamptz,sqlc.arg(before_type)::text,sqlc.arg(before_id)::uuid))
ORDER BY created_at DESC,task_type DESC,task_id DESC
LIMIT sqlc.arg(page_size)::integer;
