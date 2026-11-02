-- name: ListSpeechPlaybackCacheByPrefix :many
-- Candidates for the posted opening frame. Expired rows are never returned, so
-- the 30-day retention window is enforced by reads rather than by the reaper.
SELECT cache_key, model_id, voice_identity, content_sha256, audio_sha256, audio_size,
       content_type, storage_key, provider_task_id, created_at, expires_at
FROM speech_playback_cache
WHERE team_id = public.memoh_current_team_id()
  AND bot_id = sqlc.arg(bot_id)
  AND model_id = sqlc.arg(model_id)
  AND voice_identity = sqlc.arg(voice_identity)
  AND prefix_hash = sqlc.arg(prefix_hash)
  AND expires_at > now()
ORDER BY created_at DESC
LIMIT sqlc.arg(row_limit);

-- name: UpsertSpeechPlaybackCache :one
INSERT INTO speech_playback_cache (
  bot_id, prefix_hash, cache_key, model_id, voice_identity, content_sha256, content_chars,
  audio_sha256, audio_size, content_type, storage_key, provider_task_id, expires_at
)
VALUES (
  sqlc.arg(bot_id),
  sqlc.arg(prefix_hash),
  sqlc.arg(cache_key),
  sqlc.arg(model_id),
  sqlc.arg(voice_identity),
  sqlc.arg(content_sha256),
  sqlc.arg(content_chars),
  sqlc.arg(audio_sha256),
  sqlc.arg(audio_size),
  sqlc.arg(content_type),
  sqlc.arg(storage_key),
  sqlc.arg(provider_task_id),
  sqlc.arg(expires_at)
)
ON CONFLICT (team_id, bot_id, cache_key) DO UPDATE SET
  prefix_hash = EXCLUDED.prefix_hash,
  model_id = EXCLUDED.model_id,
  voice_identity = EXCLUDED.voice_identity,
  content_sha256 = EXCLUDED.content_sha256,
  content_chars = EXCLUDED.content_chars,
  audio_sha256 = EXCLUDED.audio_sha256,
  audio_size = EXCLUDED.audio_size,
  content_type = EXCLUDED.content_type,
  storage_key = EXCLUDED.storage_key,
  provider_task_id = EXCLUDED.provider_task_id,
  created_at = now(),
  expires_at = EXCLUDED.expires_at
RETURNING id, audio_sha256, audio_size, content_type, storage_key, expires_at;

-- name: DeleteSpeechPlaybackCache :exec
-- Removes one unusable entry (missing or tampered bytes). Targeted, so a single
-- damaged file never drops every other reusable utterance.
DELETE FROM speech_playback_cache
WHERE team_id = public.memoh_current_team_id()
  AND bot_id = sqlc.arg(bot_id)
  AND cache_key = sqlc.arg(cache_key);

-- name: DeleteExpiredSpeechPlaybackCache :many
-- Reclaims metadata once the retention window has passed, so a long-running
-- deployment does not accumulate rows. Correctness does not depend on it.
DELETE FROM speech_playback_cache
WHERE team_id = public.memoh_current_team_id()
  AND expires_at <= now()
RETURNING storage_key;
