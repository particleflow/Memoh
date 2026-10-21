-- 0169_agenvo_execution_targets
-- Remove only this provider's registrations and columns. Native registrations remain.
DELETE FROM bot_remote_runtime_bindings WHERE runtime_id IN (SELECT id FROM user_runtimes WHERE provider = 'agenvo');
DELETE FROM user_runtimes WHERE provider = 'agenvo';
DROP INDEX IF EXISTS idx_user_runtimes_agenvo_config;
ALTER TABLE user_runtimes DROP COLUMN IF EXISTS provider_config;
ALTER TABLE user_runtimes DROP COLUMN IF EXISTS provider;
