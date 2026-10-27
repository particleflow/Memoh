-- A transaction-scoped exact intent: never an authorization credential and
-- never returned to clients. An exception rolls both intent and update back.
CREATE TABLE IF NOT EXISTS public.native_profile_cutover_intents (
    backend_pid integer NOT NULL,
    transaction_id xid8 NOT NULL,
    team_id uuid NOT NULL,
    owner_id uuid NOT NULL,
    runtime_id uuid NOT NULL,
    target_id uuid NOT NULL,
    instance_id text NOT NULL,
    request_id uuid NOT NULL,
    revision bigint NOT NULL,
    new_dir text NOT NULL,
    PRIMARY KEY (backend_pid, transaction_id, team_id, runtime_id)
);
ALTER TABLE public.native_profile_cutover_intents ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.native_profile_cutover_intents FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS native_profile_cutover_intents_team ON public.native_profile_cutover_intents;
CREATE POLICY native_profile_cutover_intents_team ON public.native_profile_cutover_intents
    USING (team_id = public.memoh_current_team_id())
    WITH CHECK (team_id = public.memoh_current_team_id());

CREATE OR REPLACE FUNCTION public.memoh_guard_paused_registration()
RETURNS TRIGGER LANGUAGE plpgsql VOLATILE AS $$
DECLARE permit public.native_profile_cutover_intents%ROWTYPE;
BEGIN
    IF OLD.provider IN ('agenvo','memoh') AND
       (NEW.provider IS DISTINCT FROM OLD.provider OR NEW.provider_config IS DISTINCT FROM OLD.provider_config) THEN
        PERFORM public.memoh_computer_admission_lock(OLD.user_id);
        IF EXISTS (SELECT 1 FROM public.computer_admission_gates
            WHERE team_id=OLD.team_id AND owner_id=OLD.user_id AND paused
              AND device_id=public.memoh_computer_admission_identity(OLD.provider,OLD.id,OLD.provider_config)->>'device_id'
              AND instance_id=public.memoh_computer_admission_identity(OLD.provider,OLD.id,OLD.provider_config)->>'instance_id') THEN
            DELETE FROM public.native_profile_cutover_intents
              WHERE backend_pid=pg_backend_pid() AND transaction_id=pg_current_xact_id()
                AND team_id=OLD.team_id AND owner_id=OLD.user_id AND runtime_id=OLD.id
                AND new_dir=NEW.provider_config #>> '{herdr,agent,config_dir}'
              RETURNING * INTO permit;
            IF NOT FOUND OR OLD.provider <> 'memoh' OR NEW.provider <> OLD.provider
               OR (to_jsonb(NEW)-'provider_config'-'updated_at') IS DISTINCT FROM (to_jsonb(OLD)-'provider_config'-'updated_at')
               OR jsonb_typeof(NEW.provider_config #> '{herdr,agent,config_dir}') IS DISTINCT FROM 'string'
               OR jsonb_set(OLD.provider_config, '{herdr,agent,config_dir}', NEW.provider_config #> '{herdr,agent,config_dir}', false) IS DISTINCT FROM NEW.provider_config
               OR NOT EXISTS (SELECT 1 FROM public.computer_admission_gates g
                   WHERE g.team_id=OLD.team_id AND g.owner_id=OLD.user_id AND g.connection_id=OLD.id
                     AND g.device_id=OLD.id::text AND g.instance_id=permit.instance_id
                     AND g.request_id=permit.request_id AND g.revision=permit.revision AND g.paused)
               OR NOT EXISTS (SELECT 1 FROM public.bot_remote_runtime_bindings b JOIN public.bots bot
                   ON bot.team_id=b.team_id AND bot.id=b.bot_id
                   WHERE b.team_id=OLD.team_id AND b.id=permit.target_id AND b.runtime_id=OLD.id AND bot.owner_user_id=OLD.user_id)
               OR OLD.provider_config #>> '{herdr,instance_id}' IS DISTINCT FROM permit.instance_id THEN
                RAISE EXCEPTION USING ERRCODE='MC001', MESSAGE='computer admission paused';
            END IF;
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

-- Caller must validate profile/path and the expected target fingerprint before
-- entry. Exact old JSON is compared again while holding the admission lock.
CREATE OR REPLACE FUNCTION public.memoh_cutover_paused_native_profile(
    p_owner uuid, p_runtime uuid, p_target uuid, p_old jsonb, p_new jsonb,
    p_instance text, p_request uuid, p_revision bigint)
RETURNS jsonb LANGUAGE plpgsql VOLATILE AS $$
DECLARE updated public.user_runtimes%ROWTYPE; gate public.computer_admission_gates%ROWTYPE;
BEGIN
    PERFORM public.memoh_computer_admission_lock(p_owner);
    PERFORM 1 FROM public.users u JOIN public.team_members m ON m.user_id=u.id
        WHERE u.id=p_owner AND u.is_active AND m.is_active AND m.team_id=public.memoh_current_team_id()
        FOR SHARE OF u, m;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE='MC003', MESSAGE='computer admission scope unverified';
    END IF;
    IF p_revision < 1 OR p_instance IS NULL OR p_request IS NULL
       OR jsonb_typeof(p_new #> '{herdr,agent,config_dir}') IS DISTINCT FROM 'string'
       OR jsonb_set(p_old, '{herdr,agent,config_dir}', p_new #> '{herdr,agent,config_dir}', false) IS DISTINCT FROM p_new
       OR p_old #>> '{herdr,instance_id}' IS DISTINCT FROM p_instance THEN
        RAISE EXCEPTION USING ERRCODE='MC003', MESSAGE='computer admission scope unverified';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM public.computer_admission_gates g
        WHERE g.team_id=public.memoh_current_team_id() AND g.owner_id=p_owner AND g.connection_id=p_runtime
          AND g.device_id=p_runtime::text AND g.instance_id=p_instance AND g.request_id=p_request
          AND g.revision=p_revision AND g.paused) THEN
        RAISE EXCEPTION USING ERRCODE='MC002', MESSAGE='computer admission revision conflict';
    END IF;
    PERFORM 1 FROM public.bot_remote_runtime_bindings b JOIN public.bots bot
        ON bot.team_id=b.team_id AND bot.id=b.bot_id
        WHERE b.team_id=public.memoh_current_team_id() AND b.id=p_target AND b.runtime_id=p_runtime AND bot.owner_user_id=p_owner
        FOR SHARE OF b, bot;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE='MC003', MESSAGE='computer admission scope unverified';
    END IF;
    INSERT INTO public.native_profile_cutover_intents
        VALUES (pg_backend_pid(), pg_current_xact_id(), public.memoh_current_team_id(), p_owner, p_runtime,
                p_target, p_instance, p_request, p_revision, p_new #>> '{herdr,agent,config_dir}');
    UPDATE public.user_runtimes SET provider_config=p_new, updated_at=now()
        WHERE team_id=public.memoh_current_team_id() AND id=p_runtime AND user_id=p_owner
          AND provider='memoh' AND activated_at IS NOT NULL AND revoked_at IS NULL AND provider_config=p_old
        RETURNING * INTO updated;
    IF NOT FOUND OR EXISTS (SELECT 1 FROM public.native_profile_cutover_intents
        WHERE backend_pid=pg_backend_pid() AND transaction_id=pg_current_xact_id()
          AND team_id=public.memoh_current_team_id() AND runtime_id=p_runtime) THEN
        RAISE EXCEPTION USING ERRCODE='MC002', MESSAGE='computer admission revision conflict';
    END IF;
    -- Advance the existing CAS fence even though admission stays paused.
    -- Comparing only the config permits A -> B -> A to revive an old request.
    UPDATE public.computer_admission_gates SET revision=revision+1, updated_at=now()
        WHERE team_id=public.memoh_current_team_id() AND owner_id=p_owner
          AND connection_id=p_runtime AND device_id=p_runtime::text
          AND instance_id=p_instance AND request_id=p_request AND revision=p_revision AND paused
        RETURNING * INTO gate;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE='MC002', MESSAGE='computer admission revision conflict';
    END IF;
    RETURN to_jsonb(gate);
END;
$$;
