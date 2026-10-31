-- name: ReadConversationPresenceFacts :one
SELECT
  -- Storage role alone cannot distinguish a direct user from machine input.
  -- Return only provenance and time; the shared source validator resolves it.
  COALESCE((SELECT jsonb_agg(jsonb_build_object('source', h.metadata->'input_source', 'created_at', h.created_at)) FROM bot_history_messages h WHERE h.team_id = public.memoh_current_team_id() AND h.session_id = sqlc.arg(session_id) AND h.role = 'user' AND h.turn_id IS DISTINCT FROM sqlc.arg(turn_id)::uuid), '[]'::jsonb)::jsonb AS previous_sources,
  EXISTS(SELECT 1 FROM user_input_requests u WHERE u.team_id = public.memoh_current_team_id() AND u.session_id = sqlc.arg(session_id) AND u.status = 'pending') OR
  EXISTS(SELECT 1 FROM tool_approval_requests a WHERE a.team_id = public.memoh_current_team_id() AND a.session_id = sqlc.arg(session_id) AND a.status = 'pending') AS pending_input,
  NOT EXISTS(SELECT 1 FROM session_runs r WHERE r.team_id = public.memoh_current_team_id() AND r.session_id = sqlc.arg(session_id) AND r.run_id <> sqlc.arg(run_id)::uuid AND r.state IN ('accepted','running','waiting_decision','finishing')) AS previous_settled;

-- name: InsertConversationPresence :one
INSERT INTO conversation_presence (bot_id, session_id, run_id, invocation_id, actor_id, episode_id, generation, live_generation, fencing_token, temperature, created_at)
VALUES (sqlc.arg(bot_id), sqlc.arg(session_id), sqlc.arg(run_id), sqlc.arg(invocation_id), sqlc.arg(actor_id), sqlc.arg(episode_id), sqlc.arg(generation), sqlc.arg(live_generation), sqlc.arg(fencing_token), sqlc.arg(temperature), sqlc.arg(admitted_at)::timestamptz)
ON CONFLICT (team_id, session_id, invocation_id) DO NOTHING
RETURNING *;

-- name: LatestConversationPresence :one
SELECT * FROM conversation_presence WHERE team_id = public.memoh_current_team_id() AND session_id = sqlc.arg(session_id) AND actor_id = sqlc.arg(actor_id)
AND run_id <> sqlc.arg(run_id)::uuid
ORDER BY created_at DESC, invocation_id DESC LIMIT 1;

-- name: ClaimConversationPresence :execrows
UPDATE conversation_presence p SET state = 'claimed', claimed_at = now(), updated_at = now()
WHERE p.team_id = public.memoh_current_team_id() AND p.session_id = sqlc.arg(session_id) AND p.invocation_id = sqlc.arg(invocation_id)
AND p.episode_id = sqlc.arg(episode_id) AND p.actor_id = sqlc.arg(actor_id) AND p.generation = sqlc.arg(generation) AND p.fencing_token = sqlc.arg(fencing_token)
AND p.temperature = 'cold' AND p.state = 'eligible' AND p.claimed_at IS NULL
AND EXISTS (SELECT 1 FROM session_runs r WHERE r.team_id = p.team_id AND r.run_id = p.run_id AND r.owner_id = sqlc.arg(owner_id) AND r.live_generation = p.live_generation AND r.fencing_token = p.fencing_token AND r.state = 'running' AND r.abort_requested_at IS NULL)
AND NOT EXISTS (SELECT 1 FROM conversation_presence c WHERE c.team_id = p.team_id AND c.session_id = p.session_id AND c.episode_id = p.episode_id AND c.claimed_at IS NOT NULL);

-- name: PublishConversationPresence :execrows
UPDATE conversation_presence p SET state = 'published', piece = sqlc.arg(piece)::jsonb, updated_at = now()
WHERE p.team_id = public.memoh_current_team_id() AND p.session_id = sqlc.arg(session_id) AND p.invocation_id = sqlc.arg(invocation_id)
AND p.episode_id = sqlc.arg(episode_id) AND p.actor_id = sqlc.arg(actor_id) AND p.generation = sqlc.arg(generation) AND p.fencing_token = sqlc.arg(fencing_token) AND p.state = 'claimed'
AND EXISTS (SELECT 1 FROM session_runs r WHERE r.team_id = p.team_id AND r.run_id = p.run_id AND r.owner_id = sqlc.arg(owner_id) AND r.live_generation = p.live_generation AND r.fencing_token = p.fencing_token AND r.state = 'running' AND r.abort_requested_at IS NULL);

-- name: CloseConversationPresenceRun :execrows
INSERT INTO conversation_presence (bot_id, session_id, run_id, invocation_id, actor_id, episode_id, generation, live_generation, fencing_token, temperature, state, close_reason, created_at)
SELECT r.bot_id, r.session_id, r.run_id, r.invocation_id, sqlc.arg(actor_id), r.run_id, sqlc.arg(generation), r.live_generation, sqlc.arg(fencing_token), 'ineligible', 'closed', sqlc.arg(reason), r.created_at
FROM session_runs r WHERE r.team_id = public.memoh_current_team_id() AND r.bot_id = sqlc.arg(bot_id) AND r.session_id = sqlc.arg(session_id) AND r.run_id = sqlc.arg(run_id) AND r.fencing_token = sqlc.arg(fencing_token)
ON CONFLICT (team_id, session_id, invocation_id) DO UPDATE SET state = 'closed', close_reason = EXCLUDED.close_reason, updated_at = now()
WHERE conversation_presence.actor_id = EXCLUDED.actor_id AND conversation_presence.generation = EXCLUDED.generation AND conversation_presence.live_generation = EXCLUDED.live_generation AND conversation_presence.fencing_token = EXCLUDED.fencing_token;
