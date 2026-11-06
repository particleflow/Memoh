-- Incoming controls and their observed server return are separate facts.
-- Existing records remain unknown; never backfill a result from terminal state.
ALTER TABLE public.session_run_abort_controls
    ADD COLUMN IF NOT EXISTS completed_at timestamptz,
    ADD COLUMN IF NOT EXISTS applied boolean,
    ADD COLUMN IF NOT EXISTS error_class text,
    ADD COLUMN IF NOT EXISTS server_source text;
ALTER TABLE public.session_run_abort_controls DROP CONSTRAINT IF EXISTS session_abort_outcome_shape;
ALTER TABLE public.session_run_abort_controls ADD CONSTRAINT session_abort_outcome_shape CHECK (
    (completed_at IS NULL AND applied IS NULL AND error_class IS NULL AND server_source IS NULL)
    OR (completed_at IS NOT NULL AND applied IS NOT NULL AND error_class IS NOT NULL AND server_source IS NOT NULL
        AND error_class IN ('none','canceled','deadline_exceeded','control_error')
        AND server_source IN ('agent_service','runtime_controller'))
);
