-- 0187_speech_playback_cache
-- E2d: reuse synthesized speech for unchanged content instead of opening a
-- fresh provider synthesis on every read-aloud request. The row is the durable
-- authority; the audio bytes live beside the data root (storage_key) and are
-- verified against audio_sha256 before a hit is served.
--
-- prefix_hash identifies the first input frame the client posts, which is all
-- the server knows before any audio is expected. content_sha256 identifies the
-- whole utterance that actually produced the bytes; a hit requires both, so two
-- replies that merely share an opening sentence never exchange audio.
--
-- provider_task_id records the provider-side task for audit only: the DashScope
-- duplex task is socket-scoped and cannot be dereferenced after the socket
-- closes, which is why the encoded bytes are retained instead of a task id.
CREATE TABLE IF NOT EXISTS public.speech_playback_cache (
  team_id UUID NOT NULL DEFAULT public.memoh_current_team_id() REFERENCES public.teams(id) ON DELETE CASCADE,
  id UUID NOT NULL DEFAULT gen_random_uuid(),
  bot_id UUID NOT NULL,
  prefix_hash TEXT NOT NULL,
  cache_key TEXT NOT NULL,
  model_id UUID NOT NULL,
  voice_identity TEXT NOT NULL,
  content_sha256 TEXT NOT NULL,
  content_chars INTEGER NOT NULL,
  audio_sha256 TEXT NOT NULL,
  audio_size BIGINT NOT NULL CHECK (audio_size >= 0),
  content_type TEXT NOT NULL,
  storage_key TEXT NOT NULL,
  provider_task_id TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (team_id, id),
  CONSTRAINT speech_playback_cache_key_unique UNIQUE (team_id, bot_id, cache_key),
  FOREIGN KEY (team_id, bot_id) REFERENCES public.bots(team_id, id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS speech_playback_cache_prefix
  ON public.speech_playback_cache(team_id, bot_id, model_id, voice_identity, prefix_hash, created_at DESC);
CREATE INDEX IF NOT EXISTS speech_playback_cache_expiry
  ON public.speech_playback_cache(team_id, expires_at);
ALTER TABLE public.speech_playback_cache ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.speech_playback_cache FORCE ROW LEVEL SECURITY;
-- Policies are created idempotently: upgrade-path tests step the chain back to
-- an earlier number and migrate up again, which replays this file over a table
-- that already carries its policies.
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'speech_playback_cache' AND policyname = 'speech_playback_cache_team_select') THEN
    CREATE POLICY speech_playback_cache_team_select ON public.speech_playback_cache
      FOR SELECT USING (team_id = public.memoh_current_team_id());
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'speech_playback_cache' AND policyname = 'speech_playback_cache_team_insert') THEN
    CREATE POLICY speech_playback_cache_team_insert ON public.speech_playback_cache
      FOR INSERT WITH CHECK (team_id = public.memoh_current_team_id());
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'speech_playback_cache' AND policyname = 'speech_playback_cache_team_update') THEN
    CREATE POLICY speech_playback_cache_team_update ON public.speech_playback_cache
      FOR UPDATE USING (team_id = public.memoh_current_team_id())
      WITH CHECK (team_id = public.memoh_current_team_id());
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'speech_playback_cache' AND policyname = 'speech_playback_cache_team_delete') THEN
    CREATE POLICY speech_playback_cache_team_delete ON public.speech_playback_cache
      FOR DELETE USING (team_id = public.memoh_current_team_id());
  END IF;
END $$;
