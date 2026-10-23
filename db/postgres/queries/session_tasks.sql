-- name: ListSessionTaskCandidates :many
-- Walk only owned subagent edges. Parent IDs alone never grant access.
WITH RECURSIVE owned AS (
  SELECT s.id, ARRAY[s.id] AS visited
  FROM bot_sessions s
  WHERE s.team_id=public.memoh_current_team_id()
    AND s.id=sqlc.arg(parent_id)::uuid AND s.bot_id=sqlc.arg(bot_id)::uuid
    AND s.created_by_user_id=sqlc.arg(user_id)::uuid AND s.deleted_at IS NULL
  UNION ALL
  SELECT child.id, parent.visited || child.id
  FROM bot_sessions child JOIN owned parent ON child.parent_session_id=parent.id
  WHERE child.team_id=public.memoh_current_team_id()
    AND child.bot_id=sqlc.arg(bot_id)::uuid AND child.created_by_user_id=sqlc.arg(user_id)::uuid
    AND child.type='subagent' AND child.deleted_at IS NULL
    AND NOT child.id=ANY(parent.visited) AND cardinality(parent.visited)<64
), candidates AS (
  SELECT 'subagent'::text AS task_type, s.id AS task_id, s.id AS session_id, s.created_at
  FROM bot_sessions s JOIN owned ON owned.id=s.id
  WHERE s.id<>sqlc.arg(parent_id)::uuid
  UNION ALL
  SELECT 'computer_operation'::text, op.id, op.session_id, op.created_at
  FROM computer_operations op JOIN owned ON owned.id=op.session_id
  WHERE op.team_id=public.memoh_current_team_id()
    AND op.bot_id=sqlc.arg(bot_id)::uuid AND op.user_id=sqlc.arg(user_id)::uuid
)
SELECT task_type, task_id, session_id, created_at FROM candidates
WHERE created_at <= sqlc.arg(snapshot_at)::timestamptz
  AND (NOT sqlc.arg(use_cursor)::boolean OR
       (created_at, task_type, task_id) < (sqlc.arg(before_time)::timestamptz, sqlc.arg(before_type)::text, sqlc.arg(before_id)::uuid))
  AND (NOT sqlc.arg(use_task)::boolean OR
       (task_type=sqlc.arg(task_type)::text AND task_id=sqlc.arg(task_id)::uuid))
ORDER BY created_at DESC, task_type DESC, task_id DESC
LIMIT sqlc.arg(page_size)::integer;

-- name: GetSubagentRunVisibleMessage :one
-- Never substitute an earlier assistant/run when the newest message is a tool,
-- oversized or hidden. Only the read projection needs this bounded envelope.
SELECT CASE WHEN octet_length(content::text) <= 1048576 THEN content ELSE 'null'::jsonb END::jsonb AS content
FROM bot_history_messages
WHERE team_id=public.memoh_current_team_id()
  AND bot_id=sqlc.arg(bot_id)::uuid AND session_id=sqlc.arg(session_id)::uuid
  AND run_id=sqlc.arg(run_id)::uuid AND turn_visible=true
ORDER BY turn_position DESC, turn_message_seq DESC, created_at DESC, id DESC
LIMIT 1;
