-- 0170_computer_operations
-- Durable receipts for once-only external computer submissions; no scheduler.
CREATE TABLE IF NOT EXISTS public.computer_operations (
    id UUID PRIMARY KEY,
    team_id UUID NOT NULL DEFAULT public.memoh_current_team_id() REFERENCES public.teams(id),
    bot_id UUID NOT NULL,
    session_id UUID NOT NULL,
    user_id UUID NOT NULL REFERENCES public.users(id),
    target_id UUID NOT NULL,
    run_id TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    target_fingerprint TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'dispatching' CHECK (state IN ('dispatching','submitted','unknown','completed','rejected','not_started')),
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    cancel_requested BOOLEAN NOT NULL DEFAULT false,
    revision BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	CONSTRAINT memoh_team_key_0ee62a9d9a93 UNIQUE (team_id,id),
    FOREIGN KEY (team_id, bot_id) REFERENCES public.bots(team_id,id) ON DELETE CASCADE,
    FOREIGN KEY (team_id, session_id) REFERENCES public.bot_sessions(team_id,id) ON DELETE CASCADE,
    UNIQUE (team_id,bot_id,session_id,user_id,run_id,fingerprint)
);
-- Uncertain submissions keep the slot: neither a new tool call ID nor a
-- restarted Memoh process can turn uncertainty into another dispatch.
CREATE UNIQUE INDEX IF NOT EXISTS computer_operations_unsettled
    ON public.computer_operations(team_id,bot_id,session_id,target_id)
    WHERE state IN ('dispatching','submitted','unknown');
ALTER TABLE public.computer_operations ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.computer_operations FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS computer_operations_team ON public.computer_operations;
CREATE POLICY computer_operations_team ON public.computer_operations
    USING (team_id=public.memoh_current_team_id())
    WITH CHECK (team_id=public.memoh_current_team_id());
