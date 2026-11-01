-- 0185_auth_device_sessions
-- Keep the table on downgrade: dropping it would force every device to log in
-- again, and the old binary's /auth/refresh cannot consume session refresh
-- tokens anyway. Never delete on down (same policy as 0183).
SELECT 1;
