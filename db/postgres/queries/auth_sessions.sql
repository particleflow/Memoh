-- name: CreateAuthSession :one
INSERT INTO auth_sessions (user_id, device_id, device_name, refresh_hash, expires_at)
VALUES (sqlc.arg(user_id), sqlc.arg(device_id), sqlc.arg(device_name), sqlc.arg(refresh_hash), sqlc.arg(expires_at))
RETURNING *;

-- name: GetAuthSessionByID :one
SELECT * FROM auth_sessions WHERE id = sqlc.arg(id);

-- name: GetActiveAuthSession :one
SELECT id
FROM auth_sessions
WHERE id = sqlc.arg(id)
  AND user_id = sqlc.arg(user_id)
  AND revoked_at IS NULL
  AND expires_at > now();

-- name: TouchAuthSession :execrows
UPDATE auth_sessions
SET last_used_at = now(),
    expires_at = sqlc.arg(expires_at)
WHERE id = sqlc.arg(id)
  AND revoked_at IS NULL
  AND expires_at > now();

-- name: RevokeAuthSession :execrows
UPDATE auth_sessions
SET revoked_at = now(),
    revoke_reason = sqlc.arg(revoke_reason)
WHERE id = sqlc.arg(id)
  AND user_id = sqlc.arg(user_id)
  AND revoked_at IS NULL;

-- name: RevokeAuthSessionsForUser :execrows
UPDATE auth_sessions
SET revoked_at = now(),
    revoke_reason = sqlc.arg(revoke_reason)
WHERE user_id = sqlc.arg(user_id)
  AND revoked_at IS NULL;

-- name: RevokeAuthSessionsForDevice :execrows
UPDATE auth_sessions
SET revoked_at = now(),
    revoke_reason = sqlc.arg(revoke_reason)
WHERE user_id = sqlc.arg(user_id)
  AND device_id = sqlc.arg(device_id)
  AND revoked_at IS NULL;

-- name: DeleteStaleAuthSessions :execrows
DELETE FROM auth_sessions
WHERE revoked_at < now() - interval '30 days'
   OR expires_at < now() - interval '30 days';
