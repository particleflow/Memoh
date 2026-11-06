BEGIN;
SET LOCAL memoh.team_id='44444444-4444-4444-8444-444444444444';
CREATE TEMP TABLE bot_history_messages (team_id uuid, session_id uuid, role text, turn_id uuid, metadata jsonb, created_at timestamptz) ON COMMIT DROP;
CREATE TEMP TABLE user_input_requests (team_id uuid, session_id uuid, status text) ON COMMIT DROP;
CREATE TEMP TABLE tool_approval_requests (team_id uuid, session_id uuid, status text) ON COMMIT DROP;
CREATE TEMP TABLE session_runs (team_id uuid, session_id uuid, run_id uuid, state text) ON COMMIT DROP;
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM pg_class WHERE oid IN (to_regclass('bot_history_messages'),to_regclass('user_input_requests'),to_regclass('tool_approval_requests'),to_regclass('session_runs')) AND relnamespace <> pg_my_temp_schema()) THEN RAISE EXCEPTION 'fixture unexpectedly references a permanent table'; END IF;
END $$;
INSERT INTO bot_history_messages VALUES ('44444444-4444-4444-8444-444444444444','11111111-1111-4111-8111-111111111111','user','55555555-5555-4555-8555-555555555555','{"input_source":{"kind":"schedule","bot_id":"66666666-6666-4666-8666-666666666666","session_id":"11111111-1111-4111-8111-111111111111","task_id":"77777777-7777-4777-8777-777777777777","task_kind":"schedule"}}',now()-interval '1 second');
DO $review$ DECLARE f record; BEGIN
SELECT * INTO f FROM (SELECT
  -- Storage role alone cannot distinguish a direct user from machine input.
  -- Return only provenance and time; the shared source validator resolves it.
  COALESCE((SELECT jsonb_agg(jsonb_build_object('source', h.metadata->'input_source', 'created_at', h.created_at)) FROM bot_history_messages h WHERE h.team_id = public.memoh_current_team_id() AND h.session_id = '11111111-1111-4111-8111-111111111111'::uuid AND h.role = 'user' AND h.turn_id IS DISTINCT FROM '22222222-2222-4222-8222-222222222222'::uuid::uuid), '[]'::jsonb)::jsonb AS previous_sources,
  EXISTS(SELECT 1 FROM user_input_requests u WHERE u.team_id = public.memoh_current_team_id() AND u.session_id = '11111111-1111-4111-8111-111111111111'::uuid AND u.status = 'pending') OR
  EXISTS(SELECT 1 FROM tool_approval_requests a WHERE a.team_id = public.memoh_current_team_id() AND a.session_id = '11111111-1111-4111-8111-111111111111'::uuid AND a.status = 'pending') AS pending_input,
  NOT EXISTS(SELECT 1 FROM session_runs r WHERE r.team_id = public.memoh_current_team_id() AND r.session_id = '11111111-1111-4111-8111-111111111111'::uuid AND r.run_id <> '33333333-3333-4333-8333-333333333333'::uuid::uuid AND r.state IN ('accepted','running','waiting_decision','finishing')) AS previous_settled) facts;
IF jsonb_array_length(f.previous_sources) <> 1 OR f.previous_sources->0->'source'->>'kind' <> 'schedule' OR f.previous_sources->0->'source'->>'task_id' IS NULL THEN RAISE EXCEPTION 'missing scoped machine provenance'; END IF;
IF f.pending_input OR NOT f.previous_settled THEN RAISE EXCEPTION 'guard projection changed'; END IF;
END $review$;
ROLLBACK;
