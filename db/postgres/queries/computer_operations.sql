-- name: CreateComputerOperation :one
INSERT INTO computer_operations(id,bot_id,session_id,user_id,target_id,run_id,fingerprint,target_fingerprint,details)
VALUES(sqlc.arg(id),sqlc.arg(bot_id),sqlc.arg(session_id),sqlc.arg(user_id),sqlc.arg(target_id),sqlc.arg(run_id),sqlc.arg(fingerprint),sqlc.arg(target_fingerprint),sqlc.arg(details))
ON CONFLICT DO NOTHING RETURNING *;

-- name: FindComputerOperation :one
SELECT * FROM computer_operations
WHERE team_id=public.memoh_current_team_id() AND bot_id=sqlc.arg(bot_id)
AND session_id=sqlc.arg(session_id) AND user_id=sqlc.arg(user_id)
AND ((run_id=sqlc.arg(run_id) AND fingerprint=sqlc.arg(fingerprint))
 OR (target_id=sqlc.arg(target_id) AND state IN ('dispatching','submitted','unknown')))
ORDER BY created_at LIMIT 1;

-- name: GetComputerOperation :one
SELECT * FROM computer_operations
WHERE team_id=public.memoh_current_team_id() AND id=sqlc.arg(id)
AND bot_id=sqlc.arg(bot_id) AND session_id=sqlc.arg(session_id) AND user_id=sqlc.arg(user_id);

-- name: UpdateComputerOperation :one
UPDATE computer_operations SET state=sqlc.arg(state),details=sqlc.arg(details),
cancel_requested=sqlc.arg(cancel_requested),revision=revision+1,updated_at=now()
WHERE team_id=public.memoh_current_team_id() AND id=sqlc.arg(id)
AND bot_id=sqlc.arg(bot_id) AND session_id=sqlc.arg(session_id) AND user_id=sqlc.arg(user_id)
AND revision=sqlc.arg(revision) RETURNING *;

-- name: IsManagedComputerConnection :one
SELECT EXISTS(SELECT 1 FROM user_runtimes
WHERE team_id=public.memoh_current_team_id() AND provider='agenvo'
AND (provider_config->>'connection_id'=sqlc.arg(connection_id)::text
 OR EXISTS (
   SELECT 1 FROM mcp_connections registered, mcp_connections candidate
   WHERE registered.team_id=user_runtimes.team_id AND candidate.team_id=user_runtimes.team_id
   AND registered.id::text=user_runtimes.provider_config->>'connection_id'
   AND candidate.id::text=sqlc.arg(connection_id)::text
   AND registered.type IN ('http','sse') AND candidate.type IN ('http','sse')
   AND NULLIF(rtrim(registered.config->>'url','/'),'')=rtrim(candidate.config->>'url','/')
 )));

-- name: GetOwnedAgenvoRuntime :one
SELECT * FROM user_runtimes WHERE team_id=public.memoh_current_team_id()
AND id=sqlc.arg(id) AND user_id=sqlc.arg(user_id) AND provider='agenvo' AND revoked_at IS NULL;

-- name: UpdateAgenvoRuntimeConfig :one
UPDATE user_runtimes SET provider_config=sqlc.arg(provider_config),updated_at=now()
WHERE team_id=public.memoh_current_team_id() AND id=sqlc.arg(id) AND user_id=sqlc.arg(user_id)
AND provider='agenvo' AND revoked_at IS NULL AND provider_config=sqlc.arg(expected_config)
RETURNING *;
