-- 0174_computer_delivery_entries
-- Lossless rollback only while all ledgers still fit the legacy reader.
-- After growth beyond that representation, restore a pre-upgrade backup instead;
-- never discard entries or claim that old writers can read the larger ledger.
ALTER TABLE public.computer_operations DISABLE ROW LEVEL SECURITY;
ALTER TABLE public.computer_delivery_entries DISABLE ROW LEVEL SECURITY;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM public.computer_delivery_entries GROUP BY team_id,operation_id HAVING count(*)>128)
    THEN
        RAISE EXCEPTION 'delivery history exceeds legacy rollback representation; no history removed' USING ERRCODE='23514';
    END IF;
    IF EXISTS (
        SELECT 1 FROM public.computer_delivery_entries
        GROUP BY team_id,operation_id
        HAVING max(version)>1 AND bool_or(version=1 AND entry->'reference'->>'direction'='download')
    ) THEN
        RAISE EXCEPTION 'mixed download proof versions cannot be represented by legacy ledger; no history removed' USING ERRCODE='23514';
    END IF;
    IF EXISTS (
        SELECT 1 FROM public.computer_operations o
        JOIN (SELECT team_id,operation_id,jsonb_build_object('version',max(version),'entries',jsonb_object_agg(delivery_id,entry)) AS ledger
              FROM public.computer_delivery_entries GROUP BY team_id,operation_id) e
          ON e.team_id=o.team_id AND e.operation_id=o.id
        WHERE octet_length(jsonb_set(o.details,'{delivery_ledger}',e.ledger)::text)>2097152
    ) THEN
        RAISE EXCEPTION 'delivery history exceeds legacy rollback representation; no history removed' USING ERRCODE='23514';
    END IF;
END $$;
DROP TRIGGER IF EXISTS computer_delivery_guard ON public.computer_operations;
UPDATE public.computer_operations o
SET details=jsonb_set(o.details,'{delivery_ledger}',COALESCE((
    SELECT jsonb_build_object('version',max(e.version),'entries',jsonb_object_agg(e.delivery_id,e.entry))
    FROM public.computer_delivery_entries e WHERE e.team_id=o.team_id AND e.operation_id=o.id
    HAVING count(*)>0),'{"version":4,"entries":{}}'::jsonb))
WHERE o.details->'delivery_ledger'='{"version":5,"storage":"rows"}'::jsonb;
DROP TABLE public.computer_delivery_entries;
ALTER TABLE public.computer_operations ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.computer_operations FORCE ROW LEVEL SECURITY;
CREATE OR REPLACE FUNCTION public.memoh_guard_computer_delivery() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.details ? 'delivery_ledger' THEN
        IF NOT (NEW.details ? 'delivery_ledger')
           OR jsonb_typeof(NEW.details->'delivery_ledger') IS DISTINCT FROM 'object'
           OR jsonb_typeof(NEW.details->'delivery_ledger'->'version') IS DISTINCT FROM 'number'
           OR NOT ((NEW.details->'delivery_ledger'->>'version') ~ '^[1-9][0-9]{0,8}$')
           OR NOT ((OLD.details->'delivery_ledger'->>'version') ~ '^[1-9][0-9]{0,8}$')
           OR (NEW.details->'delivery_ledger'->>'version')::bigint < (OLD.details->'delivery_ledger'->>'version')::bigint THEN
            RAISE EXCEPTION 'computer delivery ledger removal or downgrade rejected' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER computer_delivery_guard BEFORE UPDATE OF details ON public.computer_operations
    FOR EACH ROW EXECUTE FUNCTION public.memoh_guard_computer_delivery();
