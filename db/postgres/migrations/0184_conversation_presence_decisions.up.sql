-- 0184_conversation_presence_decisions
-- Fast-reply v3 decision accounting: Jev two-stage answers, pull trace and
-- timings live beside the correctness-bearing conversation_presence receipt
-- table (0182) rather than widening a deployed schema. Rows are written at
-- prepare-step2 with outcome NULL; the coordinator finish write point fills
-- the terminal outcome later (fail-open).
CREATE TABLE IF NOT EXISTS public.conversation_presence_decisions (
  team_id UUID NOT NULL DEFAULT public.memoh_current_team_id() REFERENCES public.teams(id),
  session_id UUID NOT NULL,
  invocation_id TEXT NOT NULL,
  run_id UUID NOT NULL,
  actor_id TEXT NOT NULL,
  final_mode TEXT NOT NULL CHECK (final_mode IN ('no_fast_reply','fast_reply','fallback_ack','fallback_silent')),
  direction TEXT CHECK (direction IN ('soothe','progress','eta','clarify','brief')),
  urgency INT CHECK (urgency BETWEEN 1 AND 3),
  fallback_reason TEXT CHECK (fallback_reason IN ('jev_timeout','gateway_unreachable','auth_error','bad_request','invalid_response','budget_exhausted') OR fallback_reason IS NULL),
  pulls INT NOT NULL DEFAULT 0,
  total_ms INT NOT NULL,
  detail JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (team_id, session_id, invocation_id),
  FOREIGN KEY (team_id, session_id, invocation_id)
    REFERENCES public.conversation_presence(team_id, session_id, invocation_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS conversation_presence_decisions_session_recent
  ON public.conversation_presence_decisions(team_id, session_id, actor_id, created_at DESC);
ALTER TABLE public.conversation_presence_decisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.conversation_presence_decisions FORCE ROW LEVEL SECURITY;
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'conversation_presence_decisions' AND policyname = 'conversation_presence_decisions_team') THEN
    CREATE POLICY conversation_presence_decisions_team ON public.conversation_presence_decisions
      USING (team_id = public.memoh_current_team_id()) WITH CHECK (team_id = public.memoh_current_team_id());
  END IF;
END $$;
