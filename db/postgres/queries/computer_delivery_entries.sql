-- name: GetComputerDeliveryEntry :one
SELECT e.delivery_id,e.version,e.entry,e.ordinal
FROM computer_delivery_entries e JOIN computer_operations o ON o.team_id=e.team_id AND o.id=e.operation_id
WHERE o.team_id=public.memoh_current_team_id() AND o.id=sqlc.arg(operation_id)
AND o.bot_id=sqlc.arg(bot_id) AND o.session_id=sqlc.arg(session_id) AND o.user_id=sqlc.arg(user_id)
AND e.delivery_id=sqlc.arg(delivery_id);

-- name: ListComputerDeliveryEntries :many
SELECT e.delivery_id,e.version,e.entry,e.ordinal
FROM computer_delivery_entries e JOIN computer_operations o ON o.team_id=e.team_id AND o.id=e.operation_id
WHERE o.team_id=public.memoh_current_team_id() AND o.id=sqlc.arg(operation_id)
AND o.bot_id=sqlc.arg(bot_id) AND o.session_id=sqlc.arg(session_id) AND o.user_id=sqlc.arg(user_id)
AND e.ordinal>sqlc.arg(after_ordinal)
ORDER BY e.ordinal LIMIT sqlc.arg(page_size);

-- name: PutComputerDeliveryEntry :one
WITH claimed AS (
    UPDATE computer_operations
    SET details=jsonb_set(details,'{delivery_ledger}','{"version":5,"storage":"rows"}'::jsonb),
        revision=revision+1,updated_at=now()
    WHERE team_id=public.memoh_current_team_id() AND id=sqlc.arg(operation_id)
    AND bot_id=sqlc.arg(bot_id) AND session_id=sqlc.arg(session_id) AND user_id=sqlc.arg(user_id)
    AND revision=sqlc.arg(expected_revision)
    RETURNING *
), saved AS (
    INSERT INTO computer_delivery_entries(team_id,operation_id,delivery_id,version,entry)
    SELECT team_id,id,sqlc.arg(delivery_id),sqlc.arg(entry_version),sqlc.arg(entry)::jsonb FROM claimed
    ON CONFLICT (team_id,operation_id,delivery_id) DO UPDATE SET version=EXCLUDED.version,entry=EXCLUDED.entry
    RETURNING operation_id
)
SELECT c.* FROM claimed c JOIN saved s ON s.operation_id=c.id;
