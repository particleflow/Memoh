DROP FUNCTION IF EXISTS public.memoh_cutover_paused_native_profile(uuid,uuid,uuid,jsonb,jsonb,text,uuid,bigint);

CREATE OR REPLACE FUNCTION public.memoh_guard_paused_registration()
RETURNS TRIGGER LANGUAGE plpgsql VOLATILE AS $$
BEGIN
    IF OLD.provider IN ('agenvo','memoh') AND
       (NEW.provider IS DISTINCT FROM OLD.provider OR NEW.provider_config IS DISTINCT FROM OLD.provider_config) THEN
        PERFORM public.memoh_computer_admission_lock(OLD.user_id);
        IF EXISTS (SELECT 1 FROM public.computer_admission_gates
            WHERE team_id=OLD.team_id AND owner_id=OLD.user_id AND paused
              AND device_id=public.memoh_computer_admission_identity(OLD.provider,OLD.id,OLD.provider_config)->>'device_id'
              AND instance_id=public.memoh_computer_admission_identity(OLD.provider,OLD.id,OLD.provider_config)->>'instance_id') THEN
            RAISE EXCEPTION USING ERRCODE='MC001', MESSAGE='computer admission paused';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

DROP TABLE IF EXISTS public.native_profile_cutover_intents;
