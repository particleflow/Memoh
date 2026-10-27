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
