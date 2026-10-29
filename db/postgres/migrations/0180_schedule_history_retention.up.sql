-- 0180_schedule_history_retention
-- Retain original schedule/fire identities after configuration deletion.
ALTER TABLE schedule ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
