-- 0181_schedule_result_delivery
-- Preserve origin and publication receipts on rollback. Older consumers ignore
-- these additive columns. Never drop history or re-enable/replay a plan here.
SELECT 1;
