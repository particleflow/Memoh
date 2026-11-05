-- 0188_computer_task_batches
-- Durable ordered queue for batched herdr_pi_task submissions (TF-10). One row
-- per batch; items are an ordered JSONB array transitioned under a row lock by
-- the single existing scan pass. No scheduler and no TTL: batch history is
-- retained exactly like computer_operations history.
CREATE TABLE IF NOT EXISTS public.computer_task_batches (
    id UUID PRIMARY KEY,
    team_id UUID NOT NULL DEFAULT public.memoh_current_team_id() REFERENCES public.teams(id),
    bot_id UUID NOT NULL,
    session_id UUID NOT NULL,
    user_id UUID NOT NULL REFERENCES public.users(id),
    target_id UUID NOT NULL,
    run_id TEXT NOT NULL,
    label TEXT NOT NULL DEFAULT '',
    fingerprint TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'running' CHECK (state IN ('running','blocked','completed')),
    items JSONB NOT NULL DEFAULT '[]'::jsonb,
    result_delivery JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT computer_task_batches_team_key UNIQUE (team_id,id),
    -- One submission identity per run: a replayed tool call returns this row.
    CONSTRAINT computer_task_batches_run_key UNIQUE (team_id,bot_id,session_id,user_id,run_id,fingerprint),
    FOREIGN KEY (team_id, bot_id) REFERENCES public.bots(team_id,id) ON DELETE CASCADE,
    FOREIGN KEY (team_id, session_id) REFERENCES public.bot_sessions(team_id,id) ON DELETE CASCADE
);
-- The lane is the existing unsettled-operation slot boundary, so one batch per
-- conversation/target advances at a time, oldest first.
CREATE INDEX IF NOT EXISTS computer_task_batches_lane
    ON public.computer_task_batches(team_id,bot_id,session_id,target_id,created_at)
    WHERE state IN ('running','blocked');
ALTER TABLE public.computer_task_batches ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.computer_task_batches FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS computer_task_batches_team ON public.computer_task_batches;
CREATE POLICY computer_task_batches_team ON public.computer_task_batches
    USING (team_id=public.memoh_current_team_id())
    WITH CHECK (team_id=public.memoh_current_team_id());
