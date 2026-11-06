-- 0172_runtime_transfer_credentials
-- Purpose-specific, reusable file credential on the existing device record.
ALTER TABLE user_runtimes
  ADD COLUMN IF NOT EXISTS transfer_credential_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS transfer_credential_digest TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS transfer_target_digest TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS transfer_expires_at TIMESTAMPTZ;
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_runtimes_transfer_credential
  ON user_runtimes(transfer_credential_id) WHERE transfer_credential_id <> '';
