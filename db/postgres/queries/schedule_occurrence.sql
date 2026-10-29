-- name: ClaimScheduleOccurrence :one
-- One durable fire per exact due instant, across callbacks and processes.
-- Counting is in the same statement and only the winning insert may count.
-- Serialize this authorization with pause/delete/update on the same row.
-- Under READ COMMITTED, a waiter rechecks enabled and the captured registration
-- generation after the updater commits, including a pause followed by resume.
-- A committed claim is the admission grant: pausing does not cancel it.
WITH eligible AS MATERIALIZED (
 SELECT s.* FROM schedule s
 WHERE s.team_id=public.memoh_current_team_id() AND s.id=sqlc.arg(schedule_id)::uuid
 AND s.deleted_at IS NULL AND s.enabled AND s.result_origin->>'version'='1'
 AND s.registration_generation=sqlc.arg(registration_generation)::bigint
 FOR UPDATE
), claimed AS (
 INSERT INTO schedule_logs (schedule_id,bot_id,result_origin,scheduled_for,claim_deadline,claim_state)
 SELECT s.id,s.bot_id,s.result_origin,sqlc.arg(scheduled_for)::timestamptz,
   clock_timestamp()+make_interval(secs => s.max_run_seconds), 'claimed'
 FROM eligible s
 ON CONFLICT (team_id,schedule_id,scheduled_for) WHERE scheduled_for IS NOT NULL DO NOTHING
 RETURNING *
), counted AS (
 UPDATE schedule s SET current_calls=s.current_calls+1,
 limit_disabled=CASE WHEN s.max_calls IS NOT NULL AND s.current_calls+1>=s.max_calls THEN true ELSE s.limit_disabled END,
 enabled=CASE WHEN s.max_calls IS NOT NULL AND s.current_calls+1>=s.max_calls THEN false ELSE s.enabled END,
 updated_at=now()
 FROM claimed c WHERE s.team_id=c.team_id AND s.id=c.schedule_id
 RETURNING s.id
)
SELECT c.* FROM claimed c JOIN counted n ON n.id=c.schedule_id;

-- name: BindScheduleOccurrenceSession :one
UPDATE schedule_logs l SET session_id=sqlc.arg(session_id)::uuid
WHERE l.team_id=public.memoh_current_team_id() AND l.id=sqlc.arg(fire_id)::uuid
 AND l.bot_id=sqlc.arg(bot_id)::uuid AND l.claim_state='claimed'
 AND l.session_id IS NULL AND l.completed_at IS NULL AND l.claim_deadline>clock_timestamp()
 AND EXISTS (SELECT 1 FROM bot_sessions s WHERE s.team_id=l.team_id AND s.bot_id=l.bot_id
   AND s.id=sqlc.arg(session_id)::uuid AND s.type='schedule' AND s.deleted_at IS NULL)
RETURNING *;

-- name: RecoverUnadmittedScheduleOccurrences :many
-- Expiry is the original execution budget, not a retry lease. Read the original
-- admission credential across the Bot before labeling it uncertain. Never
-- create a session/run, extend the deadline, or make this fire claimable again.
UPDATE schedule_logs l SET claim_state='unknown'
WHERE l.team_id=public.memoh_current_team_id() AND l.claim_state='claimed'
 AND l.result_origin->>'version'='1' AND l.completed_at IS NULL
 AND l.claim_deadline<=clock_timestamp()
 AND NOT EXISTS (SELECT 1 FROM session_runs r WHERE r.team_id=l.team_id AND r.bot_id=l.bot_id
  AND r.invocation_id='schedule:'||l.schedule_id::text||':'||l.id::text)
RETURNING l.id;

-- name: RefundBusyScheduleOccurrence :one
-- The trusted native gateway rejected admission as busy. Preserve this fire as
-- known non-execution, and return its reserved count at most once. Neither an
-- uncertain occurrence nor an existing original invocation can be refunded.
-- Lock order matches ClaimScheduleOccurrence and serializes explicit pauses.
WITH locked AS MATERIALIZED (
 SELECT id FROM schedule WHERE team_id=public.memoh_current_team_id()
 AND id=sqlc.arg(schedule_id)::uuid AND bot_id=sqlc.arg(bot_id)::uuid FOR UPDATE
), released AS (
 UPDATE schedule_logs l SET claim_state='busy', status='error',
 error_code='session_runtime.session_busy', completed_at=clock_timestamp()
 FROM locked s WHERE l.team_id=public.memoh_current_team_id()
 AND l.id=sqlc.arg(fire_id)::uuid AND l.schedule_id=s.id
 AND l.bot_id=sqlc.arg(bot_id)::uuid AND l.result_origin->>'version'='1'
 AND l.claim_state='claimed' AND l.completed_at IS NULL
 AND NOT EXISTS (SELECT 1 FROM session_runs r WHERE r.team_id=l.team_id AND r.bot_id=l.bot_id
   AND r.invocation_id='schedule:'||l.schedule_id::text||':'||l.id::text)
 RETURNING l.id,l.schedule_id
), refunded AS (
 UPDATE schedule s SET current_calls=GREATEST(s.current_calls-1,0),
 enabled=CASE WHEN s.limit_disabled AND s.deleted_at IS NULL
   AND (s.max_calls IS NULL OR s.current_calls-1<s.max_calls) THEN true ELSE s.enabled END,
 limit_disabled=CASE WHEN s.limit_disabled
   AND (s.max_calls IS NULL OR s.current_calls-1<s.max_calls) THEN false ELSE s.limit_disabled END,
 updated_at=now()
 FROM released r WHERE s.team_id=public.memoh_current_team_id() AND s.id=r.schedule_id
 RETURNING s.id
)
SELECT r.id FROM released r JOIN refunded s ON s.id=r.schedule_id;
