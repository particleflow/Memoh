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
UPDATE computer_operations SET state=sqlc.arg(state),
details=CASE WHEN details ? 'delivery_ledger'
  THEN sqlc.arg(details)::jsonb || jsonb_build_object('delivery_ledger', details->'delivery_ledger')
  ELSE sqlc.arg(details)::jsonb END,
cancel_requested=sqlc.arg(cancel_requested),revision=revision+1,updated_at=now()
WHERE team_id=public.memoh_current_team_id() AND id=sqlc.arg(id)
AND bot_id=sqlc.arg(bot_id) AND session_id=sqlc.arg(session_id) AND user_id=sqlc.arg(user_id)
AND revision=sqlc.arg(revision) RETURNING *;

-- name: UpdateComputerDelivery :one
UPDATE computer_operations
SET details=jsonb_set(details, '{delivery_ledger}', sqlc.arg(delivery_ledger)::jsonb),
revision=revision+1, updated_at=now()
WHERE team_id=public.memoh_current_team_id() AND id=sqlc.arg(id)
AND bot_id=sqlc.arg(bot_id) AND session_id=sqlc.arg(session_id) AND user_id=sqlc.arg(user_id)
AND revision=sqlc.arg(revision) RETURNING *;

-- name: ListComputerConnectionEndpoints :many
SELECT candidate.config AS candidate_config, registered.config AS registered_config,
       (user_runtimes.provider_config->>'connection_id'=sqlc.arg(connection_id)::text) AS direct
FROM user_runtimes
JOIN mcp_connections registered ON registered.team_id=user_runtimes.team_id
 AND registered.id::text=user_runtimes.provider_config->>'connection_id'
JOIN mcp_connections candidate ON candidate.team_id=user_runtimes.team_id
 AND candidate.id::text=sqlc.arg(connection_id)::text
WHERE user_runtimes.team_id=public.memoh_current_team_id() AND user_runtimes.provider='agenvo';

-- name: GetOwnedAgenvoRuntime :one
SELECT * FROM user_runtimes WHERE team_id=public.memoh_current_team_id()
AND id=sqlc.arg(id) AND user_id=sqlc.arg(user_id) AND provider='agenvo' AND revoked_at IS NULL;

-- name: UpdateAgenvoRuntimeConfig :one
UPDATE user_runtimes SET provider_config=sqlc.arg(provider_config),updated_at=now()
WHERE team_id=public.memoh_current_team_id() AND id=sqlc.arg(id) AND user_id=sqlc.arg(user_id)
AND provider='agenvo' AND revoked_at IS NULL AND provider_config=sqlc.arg(expected_config)
RETURNING *;

-- name: PreviousComputerPiOperation :one
-- Called only after owning the unsettled slot. A missing receipt in the latest
-- operation is evidence of uncertainty, never permission to search past it.
SELECT * FROM computer_operations
WHERE team_id=public.memoh_current_team_id() AND bot_id=sqlc.arg(bot_id)
AND session_id=sqlc.arg(session_id) AND user_id=sqlc.arg(user_id)
AND target_id=sqlc.arg(target_id) AND id<>sqlc.arg(id)
AND details->'batch'->'progress'->>'pi_session_id'=sqlc.arg(pi_session_id)::text
ORDER BY created_at DESC, id DESC LIMIT 1;

-- name: GetOwnedNativeHerdrRuntime :one
SELECT * FROM user_runtimes WHERE team_id=public.memoh_current_team_id()
AND id=sqlc.arg(id) AND user_id=sqlc.arg(user_id) AND provider='memoh' AND revoked_at IS NULL
AND activated_at IS NOT NULL;

-- name: UpdateNativeHerdrRuntimeConfig :one
UPDATE user_runtimes SET provider_config=sqlc.arg(provider_config),updated_at=now()
WHERE team_id=public.memoh_current_team_id() AND id=sqlc.arg(id) AND user_id=sqlc.arg(user_id)
AND provider='memoh' AND revoked_at IS NULL AND activated_at IS NOT NULL
AND provider_config=sqlc.arg(expected_config)
RETURNING *;
