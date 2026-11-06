-- 0183_session_follow_up_queue: retain durable input and receipt identities.
-- Preserve durable queue identities and accepted/failed/unknown history.
-- An older binary ignores this table; returning to it is unsafe until pending
-- work has completed or a compatible reader is available. Never delete on down.
SELECT 1;
