-- 0182_conversation_presence
-- Keep optional conversation acknowledgement claims outside model/history input.
CREATE TABLE IF NOT EXISTS public.conversation_presence (
  team_id UUID NOT NULL DEFAULT public.memoh_current_team_id() REFERENCES public.teams(id),
  bot_id UUID NOT NULL,
  session_id UUID NOT NULL,
  run_id UUID NOT NULL,
  invocation_id TEXT NOT NULL,
  actor_id TEXT NOT NULL,
  episode_id UUID NOT NULL,
  generation TEXT NOT NULL,
  live_generation TEXT NOT NULL,
  fencing_token BIGINT NOT NULL CHECK (fencing_token > 0),
  temperature TEXT NOT NULL CHECK (temperature IN ('cold','hot','uncertain','ineligible')),
  state TEXT NOT NULL DEFAULT 'eligible' CHECK (state IN ('eligible','claimed','published','closed')),
  claimed_at TIMESTAMPTZ,
  piece JSONB,
  close_reason TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (team_id, session_id, invocation_id),
  FOREIGN KEY (team_id, bot_id) REFERENCES public.bots(team_id, id) ON DELETE CASCADE,
  FOREIGN KEY (team_id, session_id, run_id) REFERENCES public.session_runs(team_id, session_id, run_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS conversation_presence_episode_claim
  ON public.conversation_presence(team_id, session_id, episode_id) WHERE claimed_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS conversation_presence_session_recent
  ON public.conversation_presence(team_id, session_id, actor_id, created_at DESC);
ALTER TABLE public.conversation_presence ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.conversation_presence FORCE ROW LEVEL SECURITY;
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'conversation_presence' AND policyname = 'conversation_presence_team') THEN
    CREATE POLICY conversation_presence_team ON public.conversation_presence
      USING (team_id = public.memoh_current_team_id()) WITH CHECK (team_id = public.memoh_current_team_id());
  END IF;
END $$;
