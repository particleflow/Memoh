-- 0172_runtime_transfer_credentials
-- Disable device transfers before rollback; rollback invalidates credentials.
DROP INDEX IF EXISTS idx_user_runtimes_transfer_credential;
ALTER TABLE user_runtimes
  DROP COLUMN IF EXISTS transfer_expires_at,
  DROP COLUMN IF EXISTS transfer_target_digest,
  DROP COLUMN IF EXISTS transfer_credential_digest,
  DROP COLUMN IF EXISTS transfer_credential_id;
