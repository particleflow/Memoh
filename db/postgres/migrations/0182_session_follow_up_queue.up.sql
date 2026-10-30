-- 0182_session_follow_up_queue: retain durable input and receipt identities.
-- Additive durable follow-up storage. Integration must confirm this migration number.
-- No cascading deletion or TTL removes accepted/failed/unknown queue history.
CREATE TABLE IF NOT EXISTS session_follow_up_queues (
 bot_id UUID NOT NULL,
 session_id UUID NOT NULL,
 envelope BYTEA NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY (bot_id,session_id)
);
-- Rollback must retain this table and the original encryption key. An old binary
-- cannot recover these queues: do not roll it back while durable inputs remain.
