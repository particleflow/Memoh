ALTER TABLE public.session_run_abort_controls
    DROP CONSTRAINT IF EXISTS session_abort_outcome_shape,
    DROP COLUMN IF EXISTS completed_at,
    DROP COLUMN IF EXISTS applied,
    DROP COLUMN IF EXISTS error_class,
    DROP COLUMN IF EXISTS server_source;
