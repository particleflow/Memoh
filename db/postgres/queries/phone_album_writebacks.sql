-- Phone album write-back intents (M3). Identity is the existing content hash;
-- the UNIQUE key makes enqueueing idempotent. A hash enqueued again after it
-- was written returns to `pending` and moves to the end of the queue, because
-- writing a re-processed product again is a valid intent.
--
-- A repeat call never renames a queued item: the display name is set once, by
-- the call that first enqueued the hash for this pairing.
-- name: EnqueuePhoneAlbumWriteback :one
INSERT INTO public.phone_album_writebacks (user_id, pair_key, bot_id, content_hash, size_bytes, mime, name)
VALUES (sqlc.arg(user_id), sqlc.arg(pair_key), sqlc.arg(bot_id), sqlc.arg(content_hash),
        sqlc.arg(size_bytes), sqlc.arg(mime), sqlc.arg(name))
ON CONFLICT (team_id, user_id, pair_key, content_hash) DO UPDATE SET
  bot_id = EXCLUDED.bot_id,
  size_bytes = EXCLUDED.size_bytes,
  mime = EXCLUDED.mime,
  name = CASE WHEN phone_album_writebacks.name <> '' THEN phone_album_writebacks.name ELSE EXCLUDED.name END,
  created_at = CASE WHEN phone_album_writebacks.state = 'written' THEN now() ELSE phone_album_writebacks.created_at END,
  state = 'pending',
  written_at = NULL
RETURNING *;

-- The phone reads one ordered work list per owner: oldest intent first. No TTL:
-- a pending row stays until the phone reports it written.
-- name: ListPendingPhoneAlbumWritebacks :many
SELECT * FROM public.phone_album_writebacks
WHERE team_id = public.memoh_current_team_id() AND user_id = sqlc.arg(user_id)
  AND state = 'pending'
ORDER BY created_at, id;

-- Marking is idempotent: repeating it keeps the first written timestamp.
-- name: MarkPhoneAlbumWritebackWritten :one
UPDATE public.phone_album_writebacks
SET state = 'written', written_at = COALESCE(written_at, now())
WHERE team_id = public.memoh_current_team_id() AND user_id = sqlc.arg(user_id)
  AND pair_key = sqlc.arg(pair_key) AND content_hash = sqlc.arg(content_hash)
RETURNING *;
