-- 0175_computer_admission
-- Never silently reopen admission by rolling back a live pause.
-- Do not accidentally inspect only the migration caller's current RLS team.
SET LOCAL row_security = off;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.computer_admission_gates WHERE paused) THEN
        RAISE EXCEPTION 'resume computer admission with the supported owner before rollback';
    END IF;
END;
$$;
DROP TRIGGER IF EXISTS computer_admission_registration ON public.user_runtimes;
DROP TRIGGER IF EXISTS computer_admission_followup ON public.computer_operations;
DROP TRIGGER IF EXISTS computer_admission_claim ON public.computer_operations;
DROP FUNCTION IF EXISTS public.memoh_guard_paused_registration();
DROP FUNCTION IF EXISTS public.memoh_guard_computer_admission();
DROP FUNCTION IF EXISTS public.memoh_assert_computer_admission(UUID,UUID);
DROP FUNCTION IF EXISTS public.memoh_change_computer_admission(UUID,UUID,JSONB,UUID,BIGINT,BOOLEAN);
DROP FUNCTION IF EXISTS public.memoh_computer_admission_lock(UUID);
DROP TABLE IF EXISTS public.computer_admission_gates;
