-- 0191_image_workflows
-- Image workflows (M4). One workflow is one row: a user-visible name, the
-- natural-language instruction the Bot executes, the two pairing labels the
-- server mints, and one lifecycle state. A run is one batch of that workflow:
-- the instruction snapshot plus the content hashes as they stood at trigger
-- time, so editing a workflow never rewrites a run.
--
-- The album identity itself stays on the phone, exactly as 0190 requires: these
-- tables store the two pairing labels, never an album uri or album name.
CREATE TABLE IF NOT EXISTS public.image_workflows (
  team_id UUID NOT NULL DEFAULT public.memoh_current_team_id() REFERENCES public.teams(id) ON DELETE CASCADE,
  id UUID NOT NULL DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
  bot_id UUID NOT NULL,
  name TEXT NOT NULL,
  instruction TEXT NOT NULL,
  input_pair_key TEXT NOT NULL,
  output_pair_key TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active','paused','revoked')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (team_id, id),
  CONSTRAINT image_workflows_name_key UNIQUE (team_id, user_id, name),
  -- Layer 0a of loop prevention: a workflow can never read its own output.
  CONSTRAINT image_workflows_ports_isolated CHECK (input_pair_key <> output_pair_key),
  FOREIGN KEY (team_id, bot_id) REFERENCES public.bots(team_id, id) ON DELETE CASCADE
);
-- The owner reads their archive in creation order, filtered by state.
CREATE INDEX IF NOT EXISTS image_workflows_owner
  ON public.image_workflows(team_id, user_id, state);
-- Layer 0b of loop prevention: one album is written by at most one live
-- workflow, so product attribution has exactly one answer. The input side is
-- deliberately not unique: two workflows may watch one input album.
CREATE UNIQUE INDEX IF NOT EXISTS image_workflows_output_pair
  ON public.image_workflows(team_id, output_pair_key) WHERE state <> 'revoked';

CREATE TABLE IF NOT EXISTS public.image_workflow_runs (
  team_id UUID NOT NULL,
  id UUID NOT NULL DEFAULT gen_random_uuid(),
  workflow_id UUID NOT NULL,
  schedule_id UUID,
  session_id UUID,
  instruction_snapshot TEXT NOT NULL,
  files JSONB NOT NULL DEFAULT '[]'::jsonb,
  fingerprint TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'armed' CHECK (state IN ('held','armed','running','completed','failed')),
  result TEXT,
  error TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (team_id, id),
  -- Layer 0d of loop prevention: a replayed batch identity is the original run.
  CONSTRAINT image_workflow_runs_fingerprint UNIQUE (team_id, workflow_id, fingerprint),
  FOREIGN KEY (team_id, workflow_id) REFERENCES public.image_workflows(team_id, id) ON DELETE CASCADE
);
-- The open-run scan: held batches are armed on resume, armed runs are watched.
CREATE INDEX IF NOT EXISTS image_workflow_runs_open
  ON public.image_workflow_runs(team_id, workflow_id, state) WHERE state IN ('held','armed','running');
-- Product attribution walks a delivery session to the run that owns it.
CREATE INDEX IF NOT EXISTS image_workflow_runs_session
  ON public.image_workflow_runs(team_id, session_id) WHERE session_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS image_workflow_runs_schedule
  ON public.image_workflow_runs(team_id, schedule_id) WHERE schedule_id IS NOT NULL;

ALTER TABLE public.image_workflows ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.image_workflows FORCE ROW LEVEL SECURITY;
ALTER TABLE public.image_workflow_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.image_workflow_runs FORCE ROW LEVEL SECURITY;
-- Policies are created idempotently: upgrade-path tests step the chain back and
-- migrate up again, which replays this file over tables that already carry
-- their policies.
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'image_workflows' AND policyname = 'image_workflows_team_select') THEN
    CREATE POLICY image_workflows_team_select ON public.image_workflows
      FOR SELECT USING (team_id = public.memoh_current_team_id());
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'image_workflows' AND policyname = 'image_workflows_team_insert') THEN
    CREATE POLICY image_workflows_team_insert ON public.image_workflows
      FOR INSERT WITH CHECK (team_id = public.memoh_current_team_id());
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'image_workflows' AND policyname = 'image_workflows_team_update') THEN
    CREATE POLICY image_workflows_team_update ON public.image_workflows
      FOR UPDATE USING (team_id = public.memoh_current_team_id())
      WITH CHECK (team_id = public.memoh_current_team_id());
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'image_workflows' AND policyname = 'image_workflows_team_delete') THEN
    CREATE POLICY image_workflows_team_delete ON public.image_workflows
      FOR DELETE USING (team_id = public.memoh_current_team_id());
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'image_workflow_runs' AND policyname = 'image_workflow_runs_team_select') THEN
    CREATE POLICY image_workflow_runs_team_select ON public.image_workflow_runs
      FOR SELECT USING (team_id = public.memoh_current_team_id());
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'image_workflow_runs' AND policyname = 'image_workflow_runs_team_insert') THEN
    CREATE POLICY image_workflow_runs_team_insert ON public.image_workflow_runs
      FOR INSERT WITH CHECK (team_id = public.memoh_current_team_id());
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'image_workflow_runs' AND policyname = 'image_workflow_runs_team_update') THEN
    CREATE POLICY image_workflow_runs_team_update ON public.image_workflow_runs
      FOR UPDATE USING (team_id = public.memoh_current_team_id())
      WITH CHECK (team_id = public.memoh_current_team_id());
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'image_workflow_runs' AND policyname = 'image_workflow_runs_team_delete') THEN
    CREATE POLICY image_workflow_runs_team_delete ON public.image_workflow_runs
      FOR DELETE USING (team_id = public.memoh_current_team_id());
  END IF;
END $$;

-- album_pairs gains the workflow role. A row with workflow_id IS NULL keeps
-- today's meaning exactly: one bare automatic write-back target per (user, bot).
ALTER TABLE public.album_pairs ADD COLUMN IF NOT EXISTS workflow_id UUID;
ALTER TABLE public.album_pairs ADD COLUMN IF NOT EXISTS role TEXT;
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'album_pairs_role_check' AND conrelid = 'public.album_pairs'::regclass) THEN
    ALTER TABLE public.album_pairs
      ADD CONSTRAINT album_pairs_role_check
      CHECK ((workflow_id IS NULL AND role IS NULL) OR (workflow_id IS NOT NULL AND role IN ('input','output')));
  END IF;
END $$;
-- The single-target rule moves up to the workflow level instead of being
-- replaced: bare rows keep one active row per (user, bot), workflow rows get one
-- active row per role.
DROP INDEX IF EXISTS public.album_pairs_active_bot;
CREATE UNIQUE INDEX IF NOT EXISTS album_pairs_active_bot
  ON public.album_pairs(team_id, user_id, bot_id) WHERE state = 'active' AND workflow_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS album_pairs_active_workflow
  ON public.album_pairs(team_id, user_id, workflow_id, role) WHERE state = 'active' AND workflow_id IS NOT NULL;
