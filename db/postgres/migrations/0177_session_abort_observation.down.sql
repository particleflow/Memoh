-- 0177_session_abort_observation
-- Remove only control observations; preserve runtime intent and history.
DROP TABLE IF EXISTS public.session_run_abort_controls;
