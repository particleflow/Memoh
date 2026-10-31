-- 0180_schedule_history_retention
-- Archived configurations become visible again, but stay disabled. Logs remain.
ALTER TABLE schedule DROP COLUMN IF EXISTS deleted_at;
