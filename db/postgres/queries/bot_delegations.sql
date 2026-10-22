-- name: CreateBotDelegation :one
INSERT INTO bot_delegations(id,team_id,user_id,source_bot_id,source_session_id,source_root_id,target_bot_id,request_id,run_id,fingerprint,source_selection,payload)
VALUES(sqlc.arg(id),sqlc.arg(team_id),sqlc.arg(user_id),sqlc.arg(source_bot_id),sqlc.arg(source_session_id),sqlc.arg(source_root_id),sqlc.arg(target_bot_id),sqlc.arg(request_id),sqlc.arg(run_id),sqlc.arg(fingerprint),sqlc.arg(source_selection),sqlc.arg(payload))
ON CONFLICT DO NOTHING RETURNING *;

-- name: FindBotDelegation :one
SELECT * FROM bot_delegations WHERE team_id=public.memoh_current_team_id()
AND source_bot_id=sqlc.arg(source_bot_id) AND source_session_id=sqlc.arg(source_session_id) AND user_id=sqlc.arg(user_id)
AND (request_id=sqlc.arg(request_id) OR (target_bot_id=sqlc.arg(target_bot_id) AND fingerprint=sqlc.arg(fingerprint) AND state IN ('accepted','running','unknown')))
ORDER BY (request_id=sqlc.arg(request_id)) DESC,created_at LIMIT 1;

-- name: GetBotDelegation :one
SELECT * FROM bot_delegations WHERE team_id=public.memoh_current_team_id()
AND id=sqlc.arg(id) AND source_bot_id=sqlc.arg(source_bot_id) AND source_session_id=sqlc.arg(source_session_id) AND user_id=sqlc.arg(user_id);

-- name: GetBotDelegationForTargetSession :one
SELECT * FROM bot_delegations WHERE team_id=public.memoh_current_team_id()
AND target_bot_id=sqlc.arg(target_bot_id) AND target_session_id=sqlc.arg(target_session_id);

-- name: UpdateBotDelegation :one
UPDATE bot_delegations SET target_session_id=sqlc.narg(target_session_id),run_id=sqlc.arg(run_id),state=sqlc.arg(state),delivery_state=sqlc.arg(delivery_state),result=sqlc.arg(result),revision=revision+1,updated_at=now()
WHERE team_id=public.memoh_current_team_id() AND id=sqlc.arg(id) AND user_id=sqlc.arg(user_id)
AND revision=sqlc.arg(revision) AND (target_session_id IS NULL OR target_session_id=sqlc.narg(target_session_id))
RETURNING *;

-- name: ListBotDelegations :many
SELECT * FROM bot_delegations WHERE team_id=public.memoh_current_team_id()
AND source_bot_id=sqlc.arg(source_bot_id) AND source_session_id=sqlc.arg(source_session_id) AND user_id=sqlc.arg(user_id)
AND created_at <= sqlc.arg(snapshot_at)
AND (NOT sqlc.arg(use_cursor)::bool OR (created_at,id)<(sqlc.arg(before_time)::timestamptz,sqlc.arg(before_id)::uuid))
ORDER BY created_at DESC,id DESC LIMIT sqlc.arg(page_size);

-- name: DelegationBotTeam :one
SELECT team_id FROM bots WHERE id=sqlc.arg(id) AND team_id=public.memoh_current_team_id();

-- name: DelegationMountForRuntime :one
SELECT id FROM bot_remote_runtime_bindings WHERE team_id=public.memoh_current_team_id()
AND bot_id=sqlc.arg(bot_id) AND runtime_id=sqlc.arg(runtime_id);

-- name: DelegationResultAssets :many
WITH RECURSIVE owned AS (
  SELECT root.id,root.team_id,ARRAY[root.id] AS ancestry FROM bot_sessions root
  WHERE root.team_id=public.memoh_current_team_id() AND root.id=sqlc.arg(root_id)
    AND root.bot_id=sqlc.arg(bot_id) AND root.created_by_user_id=sqlc.arg(user_id) AND root.deleted_at IS NULL
  UNION ALL
  SELECT child.id,child.team_id,parent.ancestry||child.id FROM bot_sessions child
  JOIN owned parent ON child.parent_session_id=parent.id AND child.team_id=parent.team_id
  WHERE child.bot_id=sqlc.arg(bot_id) AND child.created_by_user_id=sqlc.arg(user_id)
    AND child.type='subagent' AND child.deleted_at IS NULL
    AND cardinality(parent.ancestry)<64 AND NOT child.id=ANY(parent.ancestry)
)
SELECT DISTINCT asset.content_hash FROM owned
JOIN bot_history_messages message ON message.session_id=owned.id AND message.team_id=owned.team_id
JOIN bot_history_message_assets asset ON asset.message_id=message.id AND asset.team_id=owned.team_id
WHERE message.bot_id=sqlc.arg(bot_id) AND message.role='assistant' AND asset.role='attachment'
ORDER BY asset.content_hash LIMIT 256;

-- name: DelegationOutputReferences :many
SELECT * FROM bot_delegations WHERE team_id=public.memoh_current_team_id()
AND source_bot_id=sqlc.arg(source_bot_id) AND source_session_id=sqlc.arg(source_session_id)
AND user_id=sqlc.arg(user_id) AND state='completed' ORDER BY created_at DESC LIMIT 200;

-- name: DelegationFinalMessage :one
SELECT id,role,content FROM bot_history_messages WHERE team_id=public.memoh_current_team_id()
AND bot_id=sqlc.arg(bot_id) AND session_id=sqlc.arg(session_id) AND run_id=sqlc.arg(run_id)
AND turn_visible=true ORDER BY turn_position DESC,turn_message_seq DESC LIMIT 1;
