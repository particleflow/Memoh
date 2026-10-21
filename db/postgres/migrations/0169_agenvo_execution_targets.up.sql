-- 0169_agenvo_execution_targets
-- Add an Agenvo provider to the existing account computer and Bot grant model.
ALTER TABLE user_runtimes ADD COLUMN IF NOT EXISTS provider TEXT NOT NULL DEFAULT 'memoh';
ALTER TABLE user_runtimes ADD COLUMN IF NOT EXISTS provider_config JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_runtimes_agenvo_config ON user_runtimes (team_id, user_id, (md5(provider_config::text))) WHERE provider = 'agenvo' AND revoked_at IS NULL;
