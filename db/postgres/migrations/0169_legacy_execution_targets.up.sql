-- 0169_legacy_execution_targets
-- Add a legacy executor provider to the existing account computer and Bot grant model.
ALTER TABLE user_runtimes ADD COLUMN IF NOT EXISTS provider TEXT NOT NULL DEFAULT 'memoh';
ALTER TABLE user_runtimes ADD COLUMN IF NOT EXISTS provider_config JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_runtimes_legacy_config ON user_runtimes (team_id, user_id, (md5(provider_config::text))) WHERE provider = 'legacy' AND revoked_at IS NULL;
