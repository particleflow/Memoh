-- name: LockFailedUserInputRecovery :one
-- The caller holds the current recovery run's bot/session fence. Lock the
-- original facts without changing its failed state, answer or payload. A newer
-- unrelated run, a changed original, or any answer/post-answer output fences
-- recovery out; an accepted answer is not permission to replay effects.
SELECT original.run_id
FROM session_runs original
JOIN user_input_requests decision
  ON decision.team_id = original.team_id
 AND decision.run_id = original.run_id
 AND decision.bot_id = original.bot_id
 AND decision.session_id = original.session_id
WHERE original.team_id = public.memoh_current_team_id()
  AND original.run_id = sqlc.arg(original_run_id)
  AND original.bot_id = sqlc.arg(bot_id)
  AND original.session_id = sqlc.arg(session_id)
  AND original.state = 'failed'
  AND original.error_code = 'runtime_run_failed'
  AND original.fencing_token = sqlc.arg(original_fencing_token)
  AND original.updated_at = sqlc.arg(original_updated_at)
  AND original.input_json->>'kind' = 'retry_message'
  AND original.input_json->>'turn_id' = sqlc.arg(source_turn_id)::text
  AND decision.id = sqlc.arg(decision_id)
  AND decision.status = 'submitted'
  AND decision.runtime_fencing_token = original.fencing_token
  AND decision.responded_at IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM bot_history_messages message
    WHERE message.team_id = original.team_id
      AND message.session_id = original.session_id
      AND message.run_id = original.run_id
      AND message.content @> jsonb_build_object('content', jsonb_build_array(
        jsonb_build_object('type', 'tool-result', 'toolCallId', decision.tool_call_id)))
  )
  AND NOT EXISTS (
    SELECT 1 FROM session_runs newer
    WHERE newer.team_id = original.team_id
      AND newer.session_id = original.session_id
      AND newer.turn_position > original.turn_position
      AND newer.run_id <> sqlc.arg(recovery_run_id)
  )
  AND EXISTS (
    SELECT 1 FROM bot_history_messages message
    WHERE message.team_id = original.team_id
      AND message.bot_id = original.bot_id
      AND message.session_id = original.session_id
      AND message.run_id = original.run_id
      AND message.role = 'assistant'
      AND message.content @> jsonb_build_object('content', jsonb_build_array(
        jsonb_build_object('type', 'tool-call', 'toolCallId', decision.tool_call_id, 'toolName', 'ask_user')))
  )
  AND NOT EXISTS (
    SELECT 1 FROM bot_history_messages message
    WHERE message.team_id = original.team_id
      AND message.session_id = original.session_id
      AND message.run_id = original.run_id
      AND (message.turn_id IS NOT NULL OR message.turn_visible
        OR message.created_at > decision.responded_at
        OR message.role NOT IN ('assistant', 'tool')
        OR message.runtime_type <> 'model')
  )
  AND COALESCE((SELECT sum(octet_length(message.content::text))
    FROM bot_history_messages message
    WHERE message.team_id = original.team_id
      AND message.session_id = original.session_id
      AND message.run_id = original.run_id), 0) <= sqlc.arg(max_history_bytes)::bigint
FOR UPDATE OF original, decision;

-- name: ListFailedUserInputRecoveryMessages :many
SELECT * FROM bot_history_messages
WHERE team_id = public.memoh_current_team_id()
  AND bot_id = sqlc.arg(bot_id)
  AND session_id = sqlc.arg(session_id)
  AND run_id = sqlc.arg(original_run_id)
  AND turn_id IS NULL AND NOT turn_visible
ORDER BY created_at, id;

-- name: FailedUserInputRecoveryTailIsExclusive :one
-- ReplaceHistoryTurn links an unbound suffix. Do not let that existing helper
-- adopt another run's rows during a historical reconciliation.
SELECT NOT EXISTS (
  SELECT 1 FROM bot_history_messages message
  JOIN bot_history_messages anchor
    ON anchor.team_id = message.team_id AND anchor.id = sqlc.arg(assistant_id)
  WHERE message.team_id = public.memoh_current_team_id()
    AND message.session_id = sqlc.arg(session_id)
    AND message.turn_id IS NULL AND message.role IN ('assistant', 'tool')
    AND (message.created_at, message.id) >= (anchor.created_at, anchor.id)
    AND message.run_id IS DISTINCT FROM sqlc.arg(original_run_id)::uuid
) AS exclusive;
