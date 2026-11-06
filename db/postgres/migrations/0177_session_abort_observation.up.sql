-- 0177_session_abort_observation
-- Incoming controls only; no change to original run intent, state or history.
-- Received cancel controls are observations, not execution records or causes.
CREATE TABLE IF NOT EXISTS public.session_run_abort_controls (
    team_id UUID NOT NULL DEFAULT public.memoh_current_team_id(),
    run_id UUID NOT NULL,
    session_id UUID NOT NULL,
    request_hash TEXT NOT NULL CHECK (length(request_hash) = 64),
    received_at TIMESTAMPTZ NOT NULL,
    source TEXT NOT NULL CHECK (source = 'websocket'),
    client_reason TEXT NOT NULL CHECK (client_reason IN
        ('unspecified', 'user_stop', 'bot_switch', 'auth_reset', 'workdir_mismatch', 'client_cleanup')),
    PRIMARY KEY (team_id, run_id, request_hash),
    FOREIGN KEY (team_id, session_id, run_id)
        REFERENCES public.session_runs(team_id, session_id, run_id) ON DELETE CASCADE
);
ALTER TABLE public.session_run_abort_controls ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.session_run_abort_controls FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS session_run_abort_controls_team ON public.session_run_abort_controls;
CREATE POLICY session_run_abort_controls_team ON public.session_run_abort_controls
    USING (team_id = public.memoh_current_team_id())
    WITH CHECK (team_id = public.memoh_current_team_id());
