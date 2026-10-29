-- 0181_schedule_result_delivery
-- Trusted origin is host-owned; legacy plans remain unopted-in.
ALTER TABLE schedule ADD COLUMN IF NOT EXISTS result_origin JSONB NOT NULL DEFAULT '{}';
ALTER TABLE schedule_logs ADD COLUMN IF NOT EXISTS result_origin JSONB NOT NULL DEFAULT '{}';
ALTER TABLE schedule_logs ADD COLUMN IF NOT EXISTS result_delivery JSONB NOT NULL DEFAULT '{}';
