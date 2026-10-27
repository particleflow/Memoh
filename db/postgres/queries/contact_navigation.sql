-- name: ReadContactSession :one
-- Include tombstones: absence from the public session API never authorizes replacement.
SELECT * FROM bot_sessions
WHERE team_id = public.memoh_current_team_id() AND bot_id = sqlc.arg(bot_id) AND id = sqlc.arg(session_id);

-- name: ListContactCandidates :many
-- Stable ID pagination is only presentation order, never an automatic preference.
SELECT s.* FROM bot_sessions s
WHERE s.team_id = public.memoh_current_team_id() AND s.bot_id = sqlc.arg(bot_id)
  AND s.created_by_user_id = sqlc.arg(user_id) AND s.deleted_at IS NULL
  AND s.visibility = 'user' AND s.parent_session_id IS NULL AND s.route_id IS NULL
  AND COALESCE(s.channel_type, '') IN ('', 'local') AND s.type = 'chat' AND s.session_mode = 'chat' AND s.runtime_type = 'model'
  AND NOT EXISTS (SELECT 1 FROM bot_delegations d WHERE d.team_id = s.team_id AND d.target_session_id = s.id)
  AND (NOT sqlc.arg(use_cursor)::bool OR s.id > sqlc.arg(after_id)::uuid)
ORDER BY s.id LIMIT 51;

-- name: BindContactCandidate :execrows
-- Caller holds Bot -> Session -> Route locks. Repeat every eligibility predicate
-- at the write boundary; adoption does not rewrite timestamps, history or work.
UPDATE bot_sessions s SET route_id = sqlc.arg(route_id)
WHERE s.team_id = public.memoh_current_team_id() AND s.bot_id = sqlc.arg(bot_id) AND s.id = sqlc.arg(session_id)
  AND s.created_by_user_id = sqlc.arg(user_id) AND s.deleted_at IS NULL
  AND s.visibility = 'user' AND s.parent_session_id IS NULL AND s.route_id IS NULL
  AND COALESCE(s.channel_type, '') IN ('', 'local') AND s.type = 'chat' AND s.session_mode = 'chat' AND s.runtime_type = 'model'
  AND NOT EXISTS (SELECT 1 FROM bot_delegations d WHERE d.team_id = s.team_id AND d.target_session_id = s.id);

-- name: InsertContactBindingRoute :one
-- A failed insert cannot be mistaken for a corrupt existing empty pointer.
INSERT INTO bot_channel_routes(bot_id,channel_type,external_conversation_id,conversation_type,default_reply_target,metadata)
VALUES(sqlc.arg(bot_id),'web',sqlc.arg(conversation_id),'private',sqlc.arg(reply_target),'{}')
ON CONFLICT DO NOTHING RETURNING id;

-- name: GetContactCandidate :one
-- An explicitly opened old conversation may be outside the first page. Treat
-- the supplied ID only as a display preference after the same eligibility check.
SELECT s.* FROM bot_sessions s
WHERE s.team_id = public.memoh_current_team_id() AND s.bot_id = sqlc.arg(bot_id)
  AND s.id = sqlc.arg(session_id) AND s.created_by_user_id = sqlc.arg(user_id) AND s.deleted_at IS NULL
  AND s.visibility = 'user' AND s.parent_session_id IS NULL AND s.route_id IS NULL
  AND COALESCE(s.channel_type, '') IN ('', 'local') AND s.type = 'chat' AND s.session_mode = 'chat' AND s.runtime_type = 'model'
  AND NOT EXISTS (SELECT 1 FROM bot_delegations d WHERE d.team_id = s.team_id AND d.target_session_id = s.id);
