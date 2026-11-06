-- 0173_bot_delegations
-- Durable, user-authorized handoffs between existing Bot conversations.
-- Coordinate deployment with reserved transfer migrations 0171/0172.
CREATE TABLE IF NOT EXISTS public.bot_delegations (
    id UUID PRIMARY KEY,
    team_id UUID NOT NULL DEFAULT public.memoh_current_team_id() REFERENCES public.teams(id),
    user_id UUID NOT NULL REFERENCES public.users(id),
    source_bot_id UUID NOT NULL,
    source_session_id UUID NOT NULL,
    source_root_id UUID NOT NULL,
    target_bot_id UUID NOT NULL,
    target_session_id UUID,
    request_id UUID NOT NULL,
    run_id UUID NOT NULL,
    fingerprint TEXT NOT NULL,
    source_selection TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'accepted' CHECK (state IN ('accepted','running','unknown','completed','failed')),
    delivery_state TEXT NOT NULL DEFAULT 'pending' CHECK (delivery_state IN ('pending','accepted','unknown','unavailable')),
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    result JSONB NOT NULL DEFAULT '{}'::jsonb,
    revision BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (team_id,id),
    UNIQUE (team_id,source_bot_id,source_session_id,user_id,request_id),
    UNIQUE (team_id,target_session_id),
    CHECK (source_bot_id <> target_bot_id),
    FOREIGN KEY (team_id,source_bot_id) REFERENCES public.bots(team_id,id) ON DELETE CASCADE,
    FOREIGN KEY (team_id,target_bot_id) REFERENCES public.bots(team_id,id) ON DELETE CASCADE,
    FOREIGN KEY (team_id,source_session_id) REFERENCES public.bot_sessions(team_id,id) ON DELETE CASCADE,
    FOREIGN KEY (team_id,source_root_id) REFERENCES public.bot_sessions(team_id,id) ON DELETE CASCADE,
    FOREIGN KEY (team_id,target_session_id) REFERENCES public.bot_sessions(team_id,id) ON DELETE CASCADE
);
-- An unresolved identical handoff is never re-dispatched under a new request ID.
CREATE UNIQUE INDEX IF NOT EXISTS bot_delegations_unsettled
    ON public.bot_delegations(team_id,source_bot_id,source_session_id,user_id,target_bot_id,fingerprint)
    WHERE state IN ('accepted','running','unknown');
CREATE INDEX IF NOT EXISTS bot_delegations_source_created
    ON public.bot_delegations(team_id,source_bot_id,source_session_id,user_id,created_at DESC,id DESC);
ALTER TABLE public.bot_delegations ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.bot_delegations FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS bot_delegations_team ON public.bot_delegations;
CREATE POLICY bot_delegations_team ON public.bot_delegations
    USING (team_id=public.memoh_current_team_id())
    WITH CHECK (team_id=public.memoh_current_team_id());
