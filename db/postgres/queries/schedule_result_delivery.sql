-- name: ListScheduleResultFires :many
SELECT id, bot_id, result_origin FROM schedule_logs
WHERE team_id=public.memoh_current_team_id() AND result_origin->>'version'='1';

-- name: ReadScheduleResultFire :one
SELECT * FROM schedule_logs
WHERE team_id=public.memoh_current_team_id() AND bot_id=sqlc.arg(bot_id)::uuid AND id=sqlc.arg(fire_id)::uuid
AND result_origin->>'version'='1';

-- name: SaveScheduleResultDelivery :one
UPDATE schedule_logs SET result_delivery=sqlc.arg(delivery)::jsonb
WHERE team_id=public.memoh_current_team_id() AND bot_id=sqlc.arg(bot_id)::uuid AND id=sqlc.arg(fire_id)::uuid
AND result_origin=sqlc.arg(origin)::jsonb AND result_delivery=sqlc.arg(expected)::jsonb
RETURNING result_delivery;

-- name: ReadScheduleResultContact :one
SELECT r.id FROM bot_channel_routes r JOIN bot_sessions s
ON s.team_id=r.team_id AND s.bot_id=r.bot_id AND s.id=r.active_session_id AND s.route_id=r.id
WHERE r.team_id=public.memoh_current_team_id() AND r.bot_id=sqlc.arg(bot_id)::uuid
AND r.channel_type='web' AND r.external_conversation_id='contact:'||sqlc.arg(user_id)::text
AND r.conversation_type='private' AND s.id=sqlc.arg(session_id)::uuid
AND s.created_by_user_id=sqlc.arg(user_id)::uuid AND s.deleted_at IS NULL
AND s.parent_session_id IS NULL AND s.type='chat' AND s.runtime_type='model';
