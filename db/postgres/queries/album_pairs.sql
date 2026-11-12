-- Phone album pairings (M4). A pairing row records intent only: the pairing
-- label the user designated and which processing line writes into it. There is
-- no album uri, album name or device identity here; the phone's local ledger
-- resolves a label to an album on that device.
--
-- One processing line has at most one active target (partial unique index
-- album_pairs_active_bot), so activating a target first retires that line's
-- previous one. Unbinding keeps the row and its key: naming the same album
-- again reuses it.

-- name: ListAlbumPairs :many
SELECT * FROM public.album_pairs
WHERE team_id = public.memoh_current_team_id() AND user_id = sqlc.arg(user_id)
ORDER BY created_at, id;

-- name: RetireActiveAlbumPair :exec
UPDATE public.album_pairs
SET state = 'revoked', updated_at = now()
WHERE team_id = public.memoh_current_team_id()
  AND user_id = sqlc.arg(user_id) AND bot_id = sqlc.arg(bot_id)
  AND state = 'active' AND pair_key <> sqlc.arg(pair_key);

-- name: UpsertAlbumPair :one
INSERT INTO public.album_pairs (user_id, bot_id, pair_key, state)
VALUES (sqlc.arg(user_id), sqlc.arg(bot_id), sqlc.arg(pair_key), 'active')
ON CONFLICT (team_id, user_id, pair_key) DO UPDATE SET
  bot_id = EXCLUDED.bot_id,
  state = 'active',
  updated_at = CASE
    WHEN album_pairs.state = 'active' AND album_pairs.bot_id = EXCLUDED.bot_id THEN album_pairs.updated_at
    ELSE now()
  END
RETURNING *;

-- Unbinding is a soft state change: the row and its key stay, and repeating it
-- keeps the first timestamp. A label the owner never registered is not found.
-- name: RevokeAlbumPair :one
UPDATE public.album_pairs
SET state = 'revoked',
    updated_at = CASE WHEN state = 'revoked' THEN updated_at ELSE now() END
WHERE team_id = public.memoh_current_team_id() AND user_id = sqlc.arg(user_id)
  AND pair_key = sqlc.arg(pair_key)
RETURNING *;

-- name: ResolveActiveAlbumPair :one
SELECT * FROM public.album_pairs
WHERE team_id = public.memoh_current_team_id() AND user_id = sqlc.arg(user_id)
  AND bot_id = sqlc.arg(bot_id) AND state = 'active';
