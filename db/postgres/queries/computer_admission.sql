-- name: ReadComputerAdmission :one
SELECT * FROM computer_admission_gates
WHERE team_id=public.memoh_current_team_id() AND owner_id=sqlc.arg(owner_id)
  AND (connection_id=sqlc.arg(connection_id) OR paused)
  AND device_id=sqlc.arg(device_id) AND instance_id=sqlc.arg(instance_id)
ORDER BY paused DESC, (connection_id=sqlc.arg(connection_id)) DESC LIMIT 1;

-- name: ChangeComputerAdmission :one
SELECT public.memoh_change_computer_admission(
    sqlc.arg(owner_id)::uuid, sqlc.arg(runtime_id)::uuid, sqlc.arg(expected_config)::jsonb,
    sqlc.arg(request_id)::uuid, sqlc.arg(expected_revision)::bigint, sqlc.arg(paused)::boolean
)::jsonb AS receipt;

-- name: CutoverPausedNativeProfile :one
SELECT public.memoh_cutover_paused_native_profile(
    sqlc.arg(owner_id)::uuid, sqlc.arg(runtime_id)::uuid, sqlc.arg(target_id)::uuid,
    sqlc.arg(expected_config)::jsonb, sqlc.arg(proposed_config)::jsonb,
    sqlc.arg(instance_id)::text, sqlc.arg(request_id)::uuid, sqlc.arg(expected_revision)::bigint
)::jsonb AS receipt;

-- name: OwnedNativeProfileTarget :one
SELECT EXISTS (SELECT 1 FROM bot_remote_runtime_bindings b JOIN bots bot
  ON bot.team_id=b.team_id AND bot.id=b.bot_id
  WHERE b.team_id=public.memoh_current_team_id() AND b.id=sqlc.arg(target_id)
    AND b.runtime_id=sqlc.arg(runtime_id) AND bot.owner_user_id=sqlc.arg(owner_id));

-- name: ReadNativeAdmissionSnapshot :one
-- One MVCC statement sees either side of a committed cutover, never a mix.
-- Intentionally exclude api_token and all credential columns.
SELECT jsonb_build_object(
  'runtime_id',r.id,'provider',r.provider,'provider_config',r.provider_config,
  'gate',to_jsonb(g))::jsonb AS snapshot
FROM user_runtimes r
JOIN users u ON u.id=r.user_id AND u.is_active
JOIN team_members m ON m.user_id=u.id AND m.team_id=r.team_id AND m.is_active
LEFT JOIN LATERAL (
  SELECT a.* FROM computer_admission_gates a
  WHERE a.team_id=r.team_id AND a.owner_id=r.user_id
    AND (a.connection_id=r.id OR a.paused)
    AND a.device_id=public.memoh_computer_admission_identity(r.provider,r.id,r.provider_config)->>'device_id'
    AND a.instance_id=public.memoh_computer_admission_identity(r.provider,r.id,r.provider_config)->>'instance_id'
  ORDER BY a.paused DESC,(a.connection_id=r.id) DESC LIMIT 1
) g ON true
WHERE r.team_id=public.memoh_current_team_id() AND r.id=sqlc.arg(runtime_id)::uuid
  AND r.user_id=sqlc.arg(owner_id)::uuid AND r.activated_at IS NOT NULL AND r.revoked_at IS NULL;
