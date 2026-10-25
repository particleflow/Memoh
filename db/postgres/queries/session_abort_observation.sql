-- name: SessionRunMatchesAbortScope :one
-- Validate the exact target before the runtime writes an abort intent.
SELECT EXISTS (
    SELECT 1 FROM public.session_runs
    WHERE team_id = public.memoh_current_team_id()
      AND bot_id = sqlc.arg(bot_id)
      AND session_id = sqlc.arg(session_id)
      AND run_id = sqlc.arg(run_id)
);

-- name: RecordSessionRunAbortControl :execrows
-- This records a newly received request, not the cause of an old terminal.
-- A retry of the same control does not replace its original observation.
INSERT INTO public.session_run_abort_controls
    (team_id, run_id, session_id, request_hash, received_at, source, client_reason)
SELECT r.team_id, r.run_id, r.session_id, sqlc.arg(request_hash),
       sqlc.arg(received_at), 'websocket', sqlc.arg(client_reason)
FROM public.session_runs r
WHERE r.team_id = public.memoh_current_team_id()
  AND r.bot_id = sqlc.arg(bot_id)
  AND r.session_id = sqlc.arg(session_id)
  AND r.run_id = sqlc.arg(run_id)
ON CONFLICT DO NOTHING;

-- name: GetSessionRunAbortControl :one
-- Earliest observed request is not necessarily the request that caused abort.
SELECT control.received_at, control.source, control.client_reason
FROM public.session_run_abort_controls control
JOIN public.session_runs run USING (team_id, session_id, run_id)
WHERE run.team_id = public.memoh_current_team_id()
  AND run.bot_id = sqlc.arg(bot_id)
  AND run.session_id = sqlc.arg(session_id)
  AND run.run_id = sqlc.arg(run_id)
ORDER BY control.received_at, control.request_hash
LIMIT 1;
