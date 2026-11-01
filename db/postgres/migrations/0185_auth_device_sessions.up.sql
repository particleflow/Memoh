-- 0185_auth_device_sessions
-- Device sessions: one row is the device session, its static refresh credential
-- and its revocation flag. Integration must confirm this migration number.
-- Account-level like users: no team_id and no row level security.
CREATE TABLE IF NOT EXISTS auth_sessions (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  device_id     TEXT NOT NULL DEFAULT '',
  device_name   TEXT NOT NULL DEFAULT '',
  refresh_hash  TEXT NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_used_at  TIMESTAMPTZ,
  expires_at    TIMESTAMPTZ NOT NULL,
  revoked_at    TIMESTAMPTZ,
  revoke_reason TEXT,
  CONSTRAINT auth_sessions_refresh_hash_key UNIQUE (refresh_hash)
);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_user ON auth_sessions(user_id);
-- At most one live session per device: a login replaces it. Sessions without a
-- device_id stay outside the constraint so scripted logins do not displace each other.
CREATE UNIQUE INDEX IF NOT EXISTS auth_sessions_active_device ON auth_sessions(user_id, device_id)
  WHERE revoked_at IS NULL AND device_id <> '';
