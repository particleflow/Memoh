-- 0171_computer_delivery_guard
-- Remove the mixed-writer guard; only safe after all delivery writers stop.
DROP TRIGGER IF EXISTS computer_delivery_guard ON public.computer_operations;
DROP FUNCTION IF EXISTS public.memoh_guard_computer_delivery();
