-- name: ReadOwnedTransferCredential :one
SELECT runtime.id, runtime.user_id, runtime.provider_config,
 runtime.transfer_credential_id, runtime.transfer_credential_digest,
 runtime.transfer_target_digest, runtime.transfer_expires_at
FROM user_runtimes runtime
JOIN team_members membership ON membership.team_id=runtime.team_id
 AND membership.user_id=runtime.user_id AND membership.is_active
JOIN users owner ON owner.id=runtime.user_id AND owner.is_active
WHERE runtime.team_id=public.memoh_current_team_id()
 AND runtime.provider='agenvo' AND runtime.revoked_at IS NULL
 AND runtime.activated_at IS NOT NULL
 AND runtime.id=sqlc.arg(id) AND runtime.user_id=sqlc.arg(user_id);

-- name: ReadTransferCredential :one
SELECT runtime.id, runtime.user_id, runtime.provider_config,
 runtime.transfer_credential_id, runtime.transfer_credential_digest,
 runtime.transfer_target_digest, runtime.transfer_expires_at
FROM user_runtimes runtime
JOIN team_members membership ON membership.team_id=runtime.team_id
 AND membership.user_id=runtime.user_id AND membership.is_active
JOIN users owner ON owner.id=runtime.user_id AND owner.is_active
JOIN mcp_connections connection ON connection.team_id=runtime.team_id
 AND connection.id::text=runtime.provider_config->>'connection_id'
 AND connection.is_active AND connection.type='http'
JOIN bots bot ON bot.team_id=runtime.team_id AND bot.id=connection.bot_id
 AND bot.owner_user_id=runtime.user_id
WHERE runtime.team_id=public.memoh_current_team_id()
 AND runtime.provider='agenvo' AND runtime.revoked_at IS NULL
 AND runtime.activated_at IS NOT NULL
 AND runtime.transfer_credential_id=sqlc.arg(credential_id)::text
 AND runtime.transfer_credential_digest <> ''
 AND (runtime.transfer_expires_at IS NULL OR runtime.transfer_expires_at > now());

-- name: ChangeTransferCredential :execrows
-- CAS prevents a stale owner page or lost response from silently rotating again.
UPDATE user_runtimes SET transfer_credential_id=sqlc.arg(credential_id),
 transfer_credential_digest=sqlc.arg(digest), transfer_target_digest=sqlc.arg(target_digest),
 transfer_expires_at=sqlc.narg(expires_at), updated_at=now()
WHERE team_id=public.memoh_current_team_id() AND id=sqlc.arg(id)
 AND user_id=sqlc.arg(user_id) AND provider='agenvo' AND revoked_at IS NULL
 AND transfer_credential_id=sqlc.arg(expected_id)
 AND provider_config=sqlc.arg(expected_config);
