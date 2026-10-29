-- 0181_schedule_result_delivery
-- Trusted origin is host-owned; legacy plans remain unopted-in.
ALTER TABLE schedule ADD COLUMN IF NOT EXISTS result_origin JSONB NOT NULL DEFAULT '{}';
ALTER TABLE schedule_logs ADD COLUMN IF NOT EXISTS result_origin JSONB NOT NULL DEFAULT '{}';
ALTER TABLE schedule_logs ADD COLUMN IF NOT EXISTS result_delivery JSONB NOT NULL DEFAULT '{}';

-- Persist the due identity before execution-context creation. The deadline is
-- the existing execution budget; expiry cannot grant permission to replay.
ALTER TABLE schedule_logs ADD COLUMN IF NOT EXISTS scheduled_for TIMESTAMPTZ;
ALTER TABLE schedule_logs ADD COLUMN IF NOT EXISTS claim_deadline TIMESTAMPTZ;
ALTER TABLE schedule_logs ADD COLUMN IF NOT EXISTS claim_state TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_schedule_logs_occurrence
 ON schedule_logs(team_id,schedule_id,scheduled_for) WHERE scheduled_for IS NOT NULL;

-- Distinguish limit exhaustion from an explicit pause before refunding a
-- definitively unadmitted busy fire. Legacy disabled plans stay disabled.
ALTER TABLE schedule ADD COLUMN IF NOT EXISTS limit_disabled BOOLEAN NOT NULL DEFAULT false;

-- A resumed plan must not authorize callbacks captured before its pause.
-- This fences new claims only; committed fires retain their original grant.
ALTER TABLE schedule ADD COLUMN IF NOT EXISTS registration_generation BIGINT NOT NULL DEFAULT 0;
