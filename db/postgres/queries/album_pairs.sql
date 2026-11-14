-- Phone album pairings (M4). A pairing row records intent only: the pairing
-- label the user designated and which processing line writes into it. There is
-- no album uri, album name or device identity here; the phone's local ledger
-- resolves a label to an album on that device.
--
-- A row either belongs to an image workflow (workflow_id + role) or is the bare
-- automatic write-back target (workflow_id IS NULL). One bare target per
-- processing line (partial unique index album_pairs_active_bot), so activating a
-- target first retires that line's previous bare one; a workflow role retires
-- only that workflow's rows. Unbinding keeps the row and its key: naming the same
-- album again reuses it.

-- name: ListAlbumPairs :many
SELECT * FROM public.album_pairs
WHERE team_id = public.memoh_current_team_id() AND user_id = sqlc.arg(user_id)
ORDER BY created_at, id;

-- Retiring is scoped by workflow: a bare designation passes NULL and behaves
-- exactly as before, a workflow designation retires only that workflow's rows.
-- name: RetireActiveAlbumPair :exec
UPDATE public.album_pairs
SET state = 'revoked', updated_at = now()
WHERE team_id = public.memoh_current_team_id()
  AND user_id = sqlc.arg(user_id) AND bot_id = sqlc.arg(bot_id)
  AND state = 'active' AND pair_key <> sqlc.arg(pair_key)
  AND workflow_id IS NOT DISTINCT FROM sqlc.narg(workflow_id);

-- name: UpsertAlbumPair :one
INSERT INTO public.album_pairs (user_id, bot_id, pair_key, state, workflow_id, role)
VALUES (sqlc.arg(user_id), sqlc.arg(bot_id), sqlc.arg(pair_key), 'active', sqlc.narg(workflow_id), sqlc.narg(role))
ON CONFLICT (team_id, user_id, pair_key) DO UPDATE SET
  bot_id = EXCLUDED.bot_id,
  state = 'active',
  workflow_id = EXCLUDED.workflow_id,
  role = EXCLUDED.role,
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

-- The bare target lookup: the owner's single automatic write-back target. A
-- workflow-role row is never that target.
-- name: ResolveActiveAlbumPair :one
SELECT * FROM public.album_pairs
WHERE team_id = public.memoh_current_team_id() AND user_id = sqlc.arg(user_id)
  AND bot_id = sqlc.arg(bot_id) AND state = 'active'
  AND workflow_id IS NULL;

-- The workflow-attributed lookup: which label one workflow designated for one
-- role.
-- name: ResolveActiveWorkflowAlbumPair :one
SELECT * FROM public.album_pairs
WHERE team_id = public.memoh_current_team_id() AND user_id = sqlc.arg(user_id)
  AND workflow_id = sqlc.arg(workflow_id) AND role = sqlc.arg(role) AND state = 'active';

-- Revoking a workflow revokes both of its pairing rows; the rows stay, so the
-- labels stay occupied.
-- name: RevokeWorkflowAlbumPairs :exec
UPDATE public.album_pairs
SET state = 'revoked', updated_at = now()
WHERE team_id = public.memoh_current_team_id()
  AND user_id = sqlc.arg(user_id) AND workflow_id = sqlc.arg(workflow_id)
  AND state = 'active';
