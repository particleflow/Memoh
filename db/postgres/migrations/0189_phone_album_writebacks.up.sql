-- 0189_phone_album_writebacks
-- Phone album write-back queue (M3). One row is one (owner, album pairing,
-- content hash) intent: the phone pulls the pending set when the app comes to
-- the foreground, writes each item into the user-designated album, and marks
-- it written afterwards.
--
-- identity is the existing content hash: media_assets(bot_id, content_hash) is
-- the only fingerprint this queue uses. The UNIQUE key makes enqueueing
-- idempotent; a hash enqueued again after it was written returns to `pending`
-- on purpose (re-processing the same product and writing it again is a valid
-- intent). There is no TTL and no automatic expiry: a pending row stays until
-- the phone reports it written.
--
-- The album identity itself (the photo-access album uri) belongs to the phone
-- and its local ledger; it is not stored here, and no display name is used as
-- an identity anywhere in this table.
CREATE TABLE IF NOT EXISTS public.phone_album_writebacks (
  team_id UUID NOT NULL DEFAULT public.memoh_current_team_id() REFERENCES public.teams(id) ON DELETE CASCADE,
  id UUID NOT NULL DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
  pair_key TEXT NOT NULL,
  bot_id UUID NOT NULL,
  content_hash TEXT NOT NULL,
  size_bytes BIGINT,
  mime TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','written')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  written_at TIMESTAMPTZ,
  PRIMARY KEY (team_id, id),
  CONSTRAINT phone_album_writebacks_entry_key UNIQUE (team_id, user_id, pair_key, content_hash),
  FOREIGN KEY (team_id, bot_id) REFERENCES public.bots(team_id, id) ON DELETE CASCADE
);
-- The phone reads one ordered work list per owner: oldest intent first.
CREATE INDEX IF NOT EXISTS phone_album_writebacks_pending
  ON public.phone_album_writebacks(team_id, user_id, created_at)
  WHERE state = 'pending';
ALTER TABLE public.phone_album_writebacks ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.phone_album_writebacks FORCE ROW LEVEL SECURITY;
-- Policies are created idempotently: upgrade-path tests step the chain back and
-- migrate up again, which replays this file over a table that already carries
-- its policies.
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'phone_album_writebacks' AND policyname = 'phone_album_writebacks_team_select') THEN
    CREATE POLICY phone_album_writebacks_team_select ON public.phone_album_writebacks
      FOR SELECT USING (team_id = public.memoh_current_team_id());
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'phone_album_writebacks' AND policyname = 'phone_album_writebacks_team_insert') THEN
    CREATE POLICY phone_album_writebacks_team_insert ON public.phone_album_writebacks
      FOR INSERT WITH CHECK (team_id = public.memoh_current_team_id());
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'phone_album_writebacks' AND policyname = 'phone_album_writebacks_team_update') THEN
    CREATE POLICY phone_album_writebacks_team_update ON public.phone_album_writebacks
      FOR UPDATE USING (team_id = public.memoh_current_team_id())
      WITH CHECK (team_id = public.memoh_current_team_id());
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'phone_album_writebacks' AND policyname = 'phone_album_writebacks_team_delete') THEN
    CREATE POLICY phone_album_writebacks_team_delete ON public.phone_album_writebacks
      FOR DELETE USING (team_id = public.memoh_current_team_id());
  END IF;
END $$;
