-- name: EnsureSessionFollowUpQueue :exec
INSERT INTO session_follow_up_queues(bot_id,session_id,envelope)
VALUES($1,$2,$3) ON CONFLICT(bot_id,session_id) DO NOTHING;

-- name: LockSessionFollowUpQueue :one
SELECT envelope FROM session_follow_up_queues
WHERE bot_id=$1 AND session_id=$2 FOR UPDATE;

-- name: ReadSessionFollowUpQueue :one
SELECT envelope FROM session_follow_up_queues
WHERE bot_id=$1 AND session_id=$2;

-- name: WriteSessionFollowUpQueue :exec
UPDATE session_follow_up_queues SET envelope=$3,updated_at=clock_timestamp()
WHERE bot_id=$1 AND session_id=$2;

-- name: ListSessionFollowUpQueueKeys :many
SELECT bot_id::text AS bot_id,session_id::text AS session_id
FROM session_follow_up_queues ORDER BY bot_id,session_id;
