-- 0190_album_pairs
-- Phone album pairings (M4). One row is one user-designated pairing target:
-- which processing line (bot) writes finished products into which pairing
-- label, and whether automatic queueing is on.
--
-- The row stores intent only. The album identity itself (the photo-access
-- album uri) belongs to the phone and its local ledger; no uri, album name or
-- device identity is stored here, and no display name is used as an identity
-- anywhere in this table.
--
-- state is a two-value lifecycle: active turns automatic queueing on, revoked
-- is an unbind that keeps the row and its key. A revoked key stays occupied, so
-- naming the same album again reuses the same row instead of minting a second
-- identity. Unbinding never deletes queued write-back rows: an intent already
-- queued stays until the phone reports it written.
CREATE TABLE IF NOT EXISTS public.album_pairs (
  team_id UUID NOT NULL DEFAULT public.memoh_current_team_id() REFERENCES public.teams(id) ON DELETE CASCADE,
  id UUID NOT NULL DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
  bot_id UUID NOT NULL,
  pair_key TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active','revoked')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (team_id, id),
  CONSTRAINT album_pairs_key_key UNIQUE (team_id, user_id, pair_key),
  FOREIGN KEY (team_id, bot_id) REFERENCES public.bots(team_id, id) ON DELETE CASCADE
);
-- One processing line has at most one automatic write-back target. This is the
-- structural form of "the user's album" being singular, not a per-request
-- limit: setting a second target for the same bot retires the previous one.
CREATE UNIQUE INDEX IF NOT EXISTS album_pairs_active_bot
  ON public.album_pairs(team_id, user_id, bot_id) WHERE state = 'active';
CREATE INDEX IF NOT EXISTS album_pairs_owner
  ON public.album_pairs(team_id, user_id, state);
ALTER TABLE public.album_pairs ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.album_pairs FORCE ROW LEVEL SECURITY;
-- Policies are created idempotently: upgrade-path tests step the chain back and
-- migrate up again, which replays this file over a table that already carries
-- its policies.
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'album_pairs' AND policyname = 'album_pairs_team_select') THEN
    CREATE POLICY album_pairs_team_select ON public.album_pairs
      FOR SELECT USING (team_id = public.memoh_current_team_id());
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'album_pairs' AND policyname = 'album_pairs_team_insert') THEN
    CREATE POLICY album_pairs_team_insert ON public.album_pairs
      FOR INSERT WITH CHECK (team_id = public.memoh_current_team_id());
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'album_pairs' AND policyname = 'album_pairs_team_update') THEN
    CREATE POLICY album_pairs_team_update ON public.album_pairs
      FOR UPDATE USING (team_id = public.memoh_current_team_id())
      WITH CHECK (team_id = public.memoh_current_team_id());
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'album_pairs' AND policyname = 'album_pairs_team_delete') THEN
    CREATE POLICY album_pairs_team_delete ON public.album_pairs
      FOR DELETE USING (team_id = public.memoh_current_team_id());
  END IF;
END $$;
