-- 0171_computer_delivery_guard
-- Reject old operation writers that erase or downgrade a delivery ledger.
CREATE OR REPLACE FUNCTION public.memoh_guard_computer_delivery() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.details ? 'delivery_ledger' THEN
        IF NOT (NEW.details ? 'delivery_ledger')
           OR jsonb_typeof(NEW.details->'delivery_ledger') IS DISTINCT FROM 'object'
           OR jsonb_typeof(NEW.details->'delivery_ledger'->'version') IS DISTINCT FROM 'number'
           OR NOT ((NEW.details->'delivery_ledger'->>'version') ~ '^[1-9][0-9]{0,8}$')
           OR NOT ((OLD.details->'delivery_ledger'->>'version') ~ '^[1-9][0-9]{0,8}$')
           OR (NEW.details->'delivery_ledger'->>'version')::bigint <
              (OLD.details->'delivery_ledger'->>'version')::bigint THEN
            RAISE EXCEPTION 'computer delivery ledger removal or downgrade rejected'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS computer_delivery_guard ON public.computer_operations;
CREATE TRIGGER computer_delivery_guard BEFORE UPDATE OF details
    ON public.computer_operations FOR EACH ROW
    EXECUTE FUNCTION public.memoh_guard_computer_delivery();
