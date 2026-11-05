-- Batch submission identity is the submitting run plus the canonical request
-- body; a replayed tool call must never mint a second batch.
-- name: CreateComputerTaskBatch :one
INSERT INTO computer_task_batches(id,bot_id,session_id,user_id,target_id,run_id,label,fingerprint,items,state)
VALUES(sqlc.arg(id),sqlc.arg(bot_id),sqlc.arg(session_id),sqlc.arg(user_id),sqlc.arg(target_id),sqlc.arg(run_id),sqlc.arg(label),sqlc.arg(fingerprint),sqlc.arg(items),sqlc.arg(state))
ON CONFLICT DO NOTHING RETURNING *;

-- name: FindComputerTaskBatch :one
SELECT * FROM computer_task_batches
WHERE team_id=public.memoh_current_team_id() AND bot_id=sqlc.arg(bot_id)
AND session_id=sqlc.arg(session_id) AND user_id=sqlc.arg(user_id)
AND run_id=sqlc.arg(run_id) AND fingerprint=sqlc.arg(fingerprint);

-- name: GetComputerTaskBatch :one
SELECT * FROM computer_task_batches
WHERE team_id=public.memoh_current_team_id() AND id=sqlc.arg(id)
AND bot_id=sqlc.arg(bot_id) AND session_id=sqlc.arg(session_id) AND user_id=sqlc.arg(user_id);

-- name: ListActiveComputerTaskBatches :many
SELECT * FROM computer_task_batches
WHERE team_id=public.memoh_current_team_id() AND state IN ('running','blocked')
ORDER BY created_at,id;

-- name: ListCompletedComputerTaskBatches :many
SELECT * FROM computer_task_batches
WHERE team_id=public.memoh_current_team_id() AND state='completed'
ORDER BY created_at,id;

-- Item transitions run inside one transaction that holds this row lock.
-- name: LockComputerTaskBatch :one
SELECT * FROM computer_task_batches
WHERE team_id=public.memoh_current_team_id() AND id=sqlc.arg(id) FOR UPDATE;

-- name: UpdateComputerTaskBatch :one
UPDATE computer_task_batches SET items=sqlc.arg(items),state=sqlc.arg(state),updated_at=now()
WHERE team_id=public.memoh_current_team_id() AND id=sqlc.arg(id) RETURNING *;

-- name: ListSessionComputerTaskBatches :many
SELECT * FROM computer_task_batches
WHERE team_id=public.memoh_current_team_id() AND bot_id=sqlc.arg(bot_id)
AND session_id=sqlc.arg(session_id) AND user_id=sqlc.arg(user_id)
ORDER BY created_at DESC,id DESC LIMIT sqlc.arg(limit_rows);

-- Result delivery bookkeeping for the batch aggregate is a separate JSON CAS,
-- exactly like the operation/delegation/schedule result envelopes.
-- name: ReadComputerTaskBatchDelivery :one
SELECT COALESCE(result_delivery,'{}'::jsonb)::jsonb AS delivery
FROM computer_task_batches WHERE team_id=public.memoh_current_team_id() AND id=sqlc.arg(id);

-- name: SaveComputerTaskBatchDelivery :one
UPDATE computer_task_batches SET result_delivery=sqlc.arg(delivery)::jsonb,updated_at=now()
WHERE team_id=public.memoh_current_team_id() AND id=sqlc.arg(id)
AND COALESCE(result_delivery,'{}'::jsonb)=sqlc.arg(expected)::jsonb
RETURNING result_delivery AS delivery;
