-- 0182_conversation_presence
-- Reverse the optional acknowledgement ledger; never used as a data-retaining rollout rollback.
DROP TABLE IF EXISTS public.conversation_presence;
