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
