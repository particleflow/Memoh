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

-- ===== Fast-reply v3 (Jev two-stage selection): decision accounting and pull sources =====

-- name: FinalizeConversationPresenceTemperature :one
UPDATE conversation_presence p
SET temperature = sqlc.arg(temperature),
    episode_id = CASE WHEN sqlc.arg(temperature) = 'cold' THEN p.run_id ELSE p.episode_id END,
    updated_at = now()
WHERE p.team_id = public.memoh_current_team_id()
  AND p.session_id = sqlc.arg(session_id)
  AND p.invocation_id = sqlc.arg(invocation_id)
  AND p.temperature = 'uncertain' AND p.state = 'eligible'
RETURNING p.episode_id;

-- name: InsertConversationPresenceDecision :exec
INSERT INTO conversation_presence_decisions (session_id, run_id, invocation_id, actor_id, final_mode, direction, urgency, fallback_reason, pulls, total_ms, detail)
VALUES (sqlc.arg(session_id), sqlc.arg(run_id), sqlc.arg(invocation_id), sqlc.arg(actor_id), sqlc.arg(final_mode), sqlc.narg(direction), sqlc.narg(urgency), sqlc.narg(fallback_reason), sqlc.arg(pulls), sqlc.arg(total_ms), sqlc.arg(detail))
ON CONFLICT (team_id, session_id, invocation_id) DO NOTHING;

-- name: FinalizeConversationPresenceDecisionOutcome :execrows
UPDATE conversation_presence_decisions d
SET detail = d.detail || sqlc.arg(outcome_patch)::jsonb
WHERE d.team_id = public.memoh_current_team_id()
  AND d.session_id = sqlc.arg(session_id)
  AND d.invocation_id = sqlc.arg(invocation_id)
  AND NOT jsonb_exists(d.detail, 'outcome');

-- name: ReadPresenceDecisionContext :one
SELECT
  (SELECT count(*) FROM bot_history_messages h
    WHERE h.team_id = public.memoh_current_team_id() AND h.session_id = sqlc.arg(session_id)
      AND h.role IN ('user','assistant') AND h.turn_visible AND h.turn_id IS DISTINCT FROM sqlc.arg(turn_id)::uuid) AS turn_messages,
  (SELECT count(*) FROM bot_history_messages h
    WHERE h.team_id = public.memoh_current_team_id() AND h.session_id = sqlc.arg(session_id)
      AND h.role = 'assistant' AND h.turn_visible AND h.turn_id IS DISTINCT FROM sqlc.arg(turn_id)::uuid) AS assistant_messages,
  (SELECT COALESCE(sum(length(COALESCE(NULLIF(h.display_text, ''), h.content->>'content', ''))), 0) FROM bot_history_messages h
    WHERE h.team_id = public.memoh_current_team_id() AND h.session_id = sqlc.arg(session_id)
      AND h.role IN ('user','assistant') AND h.turn_visible AND h.turn_id IS DISTINCT FROM sqlc.arg(turn_id)::uuid) AS history_chars,
  COALESCE((SELECT r.state FROM session_runs r
    WHERE r.team_id = public.memoh_current_team_id() AND r.session_id = sqlc.arg(session_id)
      AND r.run_id <> sqlc.arg(run_id)::uuid AND r.state IN ('accepted','running','waiting_decision','finishing')
    ORDER BY r.created_at DESC LIMIT 1), '') AS active_run_state,
  COALESCE((SELECT GREATEST(0, floor(extract(epoch FROM (clock_timestamp() - r.created_at)))::bigint) FROM session_runs r
    WHERE r.team_id = public.memoh_current_team_id() AND r.session_id = sqlc.arg(session_id)
      AND r.run_id <> sqlc.arg(run_id)::uuid AND r.state IN ('accepted','running','waiting_decision','finishing')
    ORDER BY r.created_at DESC LIMIT 1), -1) AS active_run_age_seconds,
  COALESCE((SELECT (h.metadata->>'stop_reason') IN ('length','max_tokens') FROM bot_history_messages h
    WHERE h.team_id = public.memoh_current_team_id() AND h.session_id = sqlc.arg(session_id)
      AND h.role = 'assistant' AND h.turn_visible AND h.turn_id IS DISTINCT FROM sqlc.arg(turn_id)::uuid
    ORDER BY h.created_at DESC, h.id DESC LIMIT 1), false) AS last_reply_truncated,
  (SELECT count(*) FROM conversation_presence p
    WHERE p.team_id = public.memoh_current_team_id() AND p.session_id = sqlc.arg(session_id)
      AND p.run_id <> sqlc.arg(run_id)::uuid) AS prior_presence_rows,
  (SELECT count(*) FROM bot_history_message_assets ma
    JOIN bot_history_messages h ON h.team_id = public.memoh_current_team_id() AND h.id = ma.message_id
      AND h.session_id = sqlc.arg(session_id)) AS file_count,
  COALESCE((SELECT jsonb_agg(x.text ORDER BY x.created_at DESC) FROM (
      SELECT p2.piece->>'text' AS text, p2.created_at FROM conversation_presence p2
      WHERE p2.team_id = public.memoh_current_team_id() AND p2.session_id = sqlc.arg(session_id)
        AND p2.run_id <> sqlc.arg(run_id)::uuid AND p2.state = 'published' AND p2.piece IS NOT NULL
      ORDER BY p2.created_at DESC LIMIT 2) x), '[]'::jsonb) AS recent_ack_texts;

-- name: ReadPresenceRecentTurns :many
SELECT h.role AS role, COALESCE(NULLIF(h.display_text, ''), h.content->>'content', '') AS text, h.created_at AS created_at, h.id AS id
FROM bot_history_messages h
WHERE h.team_id = public.memoh_current_team_id() AND h.session_id = sqlc.arg(session_id)
  AND h.role IN ('user','assistant') AND h.turn_visible AND h.turn_id IS DISTINCT FROM sqlc.arg(turn_id)::uuid
  AND COALESCE(NULLIF(h.display_text, ''), h.content->>'content', '') <> ''
  AND (sqlc.narg(cursor_created_at) IS NULL OR (h.created_at, h.id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid))
ORDER BY h.created_at DESC, h.id DESC
LIMIT 6;

-- name: ReadPresenceLastReply :one
SELECT COALESCE(NULLIF(h.display_text, ''), h.content->>'content', '') AS text
FROM bot_history_messages h
WHERE h.team_id = public.memoh_current_team_id() AND h.session_id = sqlc.arg(session_id)
  AND h.role = 'assistant' AND h.turn_visible AND h.turn_id IS DISTINCT FROM sqlc.arg(turn_id)::uuid
  AND COALESCE(NULLIF(h.display_text, ''), h.content->>'content', '') <> ''
ORDER BY h.created_at DESC, h.id DESC
LIMIT 1;

-- name: ReadPresenceTaskState :one
SELECT
  COALESCE((SELECT jsonb_agg(x.entry ORDER BY x.created_at DESC) FROM (
      SELECT jsonb_build_object('state', r.state, 'age_seconds', GREATEST(0, floor(extract(epoch FROM (clock_timestamp() - r.created_at)))::bigint)) AS entry, r.created_at
      FROM session_runs r
      WHERE r.team_id = public.memoh_current_team_id() AND r.session_id = sqlc.arg(session_id)
        AND r.run_id <> sqlc.arg(run_id)::uuid AND r.state IN ('accepted','running','waiting_decision','finishing')
      ORDER BY r.created_at DESC LIMIT 3) x), '[]'::jsonb) AS runs,
  COALESCE((SELECT jsonb_agg(u.tool_name ORDER BY u.created_at) FROM (
      SELECT 'user_input' AS tool_name, u2.created_at FROM user_input_requests u2
      WHERE u2.team_id = public.memoh_current_team_id() AND u2.session_id = sqlc.arg(session_id) AND u2.status = 'pending'
      LIMIT 5) u), '[]'::jsonb) AS pending_user_input,
  COALESCE((SELECT jsonb_agg(a.tool_name ORDER BY a.created_at) FROM (
      SELECT 'approval:' || a2.tool_name AS tool_name, a2.created_at FROM tool_approval_requests a2
      WHERE a2.team_id = public.memoh_current_team_id() AND a2.session_id = sqlc.arg(session_id) AND a2.status = 'pending'
      LIMIT 5) a), '[]'::jsonb) AS pending_approvals;

-- name: ReadPresenceFileList :one
SELECT COALESCE((SELECT jsonb_agg(f.entry ORDER BY f.created_at DESC) FROM (
    SELECT jsonb_build_object('name', ma.name, 'type', ma.mime) AS entry, ma.created_at
    FROM bot_history_message_assets ma
    JOIN bot_history_messages h ON h.team_id = public.memoh_current_team_id() AND h.id = ma.message_id AND h.session_id = sqlc.arg(session_id)
    ORDER BY ma.created_at DESC LIMIT 12) f), '[]'::jsonb) AS assets;
