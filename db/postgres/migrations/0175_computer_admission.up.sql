-- 0175_computer_admission
-- Admission only: no task, receipt, result, credential or native process changes.
CREATE TABLE IF NOT EXISTS public.computer_admission_gates (
    team_id UUID NOT NULL DEFAULT public.memoh_current_team_id() REFERENCES public.teams(id),
    owner_id UUID NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    connection_id UUID NOT NULL,
    device_id TEXT NOT NULL CHECK (length(device_id) BETWEEN 1 AND 128),
    instance_id TEXT NOT NULL CHECK (length(instance_id) BETWEEN 1 AND 128),
    request_id UUID NOT NULL,
    paused BOOLEAN NOT NULL,
    revision BIGINT NOT NULL CHECK (revision > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, owner_id, connection_id, device_id, instance_id)
);
ALTER TABLE public.computer_admission_gates ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.computer_admission_gates FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS computer_admission_team ON public.computer_admission_gates;
CREATE POLICY computer_admission_team ON public.computer_admission_gates
    USING (team_id = public.memoh_current_team_id())
    WITH CHECK (team_id = public.memoh_current_team_id());

-- A short owner-scoped transaction lock also covers aliases registered under
-- other mount IDs. No lock is held while contacting Agenvo or executing work.
CREATE OR REPLACE FUNCTION public.memoh_computer_admission_lock(p_owner UUID)
RETURNS VOID LANGUAGE SQL VOLATILE AS $$
    SELECT pg_advisory_xact_lock(hashtextextended(
        'computer-admission:' || public.memoh_current_team_id()::text || ':' || p_owner::text, 0));
$$;

CREATE OR REPLACE FUNCTION public.memoh_change_computer_admission(
    p_owner UUID, p_runtime UUID, p_config JSONB,
    p_request UUID, p_revision BIGINT, p_paused BOOLEAN)
RETURNS JSONB LANGUAGE plpgsql VOLATILE AS $$
DECLARE
    registered JSONB;
    conn UUID;
    device TEXT;
    instance TEXT;
    gate public.computer_admission_gates%ROWTYPE;
BEGIN
    PERFORM public.memoh_computer_admission_lock(p_owner);
    SELECT provider_config INTO registered FROM public.user_runtimes
    WHERE team_id=public.memoh_current_team_id() AND id=p_runtime AND user_id=p_owner
      AND provider='agenvo' AND revoked_at IS NULL AND provider_config=p_config;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE='MC003', MESSAGE='computer admission scope unverified';
    END IF;
    conn := (registered->>'connection_id')::uuid;
    device := registered->>'device_id';
    instance := registered->>'instance_id';
    IF conn IS NULL OR coalesce(length(device),0) NOT BETWEEN 1 AND 128
       OR coalesce(length(instance),0) NOT BETWEEN 1 AND 128 THEN
        RAISE EXCEPTION USING ERRCODE='MC003', MESSAGE='computer admission scope unverified';
    END IF;
    -- A different MCP connection is a different trust domain unless a future
    -- native contract proves otherwise. Names, URLs and credentials are not
    -- used to guess native ownership. Refuse an ambiguous coverage claim.
    IF EXISTS (SELECT 1 FROM public.user_runtimes r
        WHERE r.team_id=public.memoh_current_team_id() AND r.user_id=p_owner
          AND r.provider='agenvo' AND r.revoked_at IS NULL
          AND r.provider_config->>'device_id'=device
          AND r.provider_config->>'instance_id'=instance
          AND lower(r.provider_config->>'connection_id') IS DISTINCT FROM conn::text) THEN
        RAISE EXCEPTION USING ERRCODE='MC003', MESSAGE='computer admission scope unverified';
    END IF;
    SELECT * INTO gate FROM public.computer_admission_gates
    WHERE team_id=public.memoh_current_team_id() AND owner_id=p_owner
      AND connection_id=conn AND device_id=device AND instance_id=instance;
    IF FOUND THEN
        IF gate.request_id=p_request AND gate.paused=p_paused THEN
            RETURN to_jsonb(gate); -- Lost acknowledgement: same transition only.
        END IF;
        IF gate.revision<>p_revision OR
           (p_paused AND (gate.paused OR gate.request_id=p_request)) OR
           (NOT p_paused AND (NOT gate.paused OR gate.request_id<>p_request)) THEN
            RAISE EXCEPTION USING ERRCODE='MC002', MESSAGE='computer admission revision conflict';
        END IF;
        UPDATE public.computer_admission_gates SET paused=p_paused, request_id=p_request,
            revision=revision+1, updated_at=now()
        WHERE team_id=gate.team_id AND owner_id=gate.owner_id AND connection_id=gate.connection_id
          AND device_id=gate.device_id AND instance_id=gate.instance_id
        RETURNING * INTO gate;
    ELSE
        IF NOT p_paused OR p_revision<>0 THEN
            RAISE EXCEPTION USING ERRCODE='MC002', MESSAGE='computer admission revision conflict';
        END IF;
        INSERT INTO public.computer_admission_gates
            (owner_id, connection_id, device_id, instance_id, request_id, paused, revision)
        VALUES (p_owner, conn, device, instance, p_request, true, 1)
        RETURNING * INTO gate;
    END IF;
    RETURN to_jsonb(gate);
END;
$$;

CREATE OR REPLACE FUNCTION public.memoh_assert_computer_admission(p_bot UUID, p_target UUID)
RETURNS VOID LANGUAGE plpgsql VOLATILE AS $$
DECLARE
    owner UUID;
    registered JSONB;
BEGIN
    SELECT owner_user_id INTO owner FROM public.bots
    WHERE team_id=public.memoh_current_team_id() AND id=p_bot;
    IF owner IS NULL THEN
        RAISE EXCEPTION USING ERRCODE='MC003', MESSAGE='computer admission scope unverified';
    END IF;
    PERFORM public.memoh_computer_admission_lock(owner);
    -- Re-read mapping after acquiring the same lock as the pause transition.
    SELECT r.provider_config INTO registered
    FROM public.bot_remote_runtime_bindings b JOIN public.user_runtimes r
      ON r.team_id=b.team_id AND r.id=b.runtime_id
    WHERE b.team_id=public.memoh_current_team_id() AND b.bot_id=p_bot AND b.id=p_target
      AND r.user_id=owner AND r.provider='agenvo';
    IF registered IS NULL OR nullif(registered->>'connection_id','') IS NULL
       OR nullif(registered->>'device_id','') IS NULL OR nullif(registered->>'instance_id','') IS NULL THEN
        -- Preserve legacy no-gate store behaviour. When the owner has a pause,
        -- an unresolvable mapping cannot establish exemption from that pause.
        IF EXISTS (SELECT 1 FROM public.computer_admission_gates
            WHERE team_id=public.memoh_current_team_id() AND owner_id=owner AND paused) THEN
            RAISE EXCEPTION USING ERRCODE='MC003', MESSAGE='computer admission scope unverified';
        END IF;
        RETURN;
    END IF;
    IF EXISTS (SELECT 1 FROM public.computer_admission_gates
        WHERE team_id=public.memoh_current_team_id() AND owner_id=owner AND paused
          AND device_id=registered->>'device_id' AND instance_id=registered->>'instance_id') THEN
        -- Include ambiguous cross-connection aliases: they must not bypass a
        -- previously closed gate. This does not attest that they are equivalent.
        RAISE EXCEPTION USING ERRCODE='MC001', MESSAGE='computer admission paused';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION public.memoh_guard_computer_admission()
RETURNS TRIGGER LANGUAGE plpgsql VOLATILE AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        IF EXISTS (SELECT 1 FROM public.computer_operations
            WHERE team_id=NEW.team_id AND bot_id=NEW.bot_id AND session_id=NEW.session_id
              AND user_id=NEW.user_id AND run_id=NEW.run_id AND fingerprint=NEW.fingerprint) THEN
            RETURN NEW; -- ON CONFLICT returns an existing receipt, never dispatches.
        END IF;
        PERFORM public.memoh_assert_computer_admission(NEW.bot_id, NEW.target_id);
    ELSIF EXISTS (SELECT 1 FROM jsonb_array_elements(coalesce(nullif(NEW.details->'followups','null'::jsonb),'[]'::jsonb)) n
        WHERE NOT EXISTS (SELECT 1 FROM jsonb_array_elements(coalesce(nullif(OLD.details->'followups','null'::jsonb),'[]'::jsonb)) o
            WHERE o->>'request_id'=n->>'request_id')) THEN
        PERFORM public.memoh_assert_computer_admission(NEW.bot_id, NEW.target_id);
    END IF;
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS computer_admission_claim ON public.computer_operations;
CREATE TRIGGER computer_admission_claim BEFORE INSERT ON public.computer_operations
    FOR EACH ROW EXECUTE FUNCTION public.memoh_guard_computer_admission();
DROP TRIGGER IF EXISTS computer_admission_followup ON public.computer_operations;
CREATE TRIGGER computer_admission_followup BEFORE UPDATE OF details ON public.computer_operations
    FOR EACH ROW EXECUTE FUNCTION public.memoh_guard_computer_admission();

CREATE OR REPLACE FUNCTION public.memoh_guard_paused_registration()
RETURNS TRIGGER LANGUAGE plpgsql VOLATILE AS $$
BEGIN
    IF OLD.provider='agenvo' AND
       (NEW.provider IS DISTINCT FROM OLD.provider OR NEW.provider_config IS DISTINCT FROM OLD.provider_config) THEN
        PERFORM public.memoh_computer_admission_lock(OLD.user_id);
        IF EXISTS (SELECT 1 FROM public.computer_admission_gates
            WHERE team_id=OLD.team_id AND owner_id=OLD.user_id AND paused
              AND device_id=OLD.provider_config->>'device_id' AND instance_id=OLD.provider_config->>'instance_id') THEN
            RAISE EXCEPTION USING ERRCODE='MC001', MESSAGE='computer admission paused';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS computer_admission_registration ON public.user_runtimes;
CREATE TRIGGER computer_admission_registration BEFORE UPDATE OF provider, provider_config ON public.user_runtimes
    FOR EACH ROW EXECUTE FUNCTION public.memoh_guard_paused_registration();
