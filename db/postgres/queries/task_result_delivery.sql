-- Result delivery is a separate JSON CAS from execution state/revision.
-- No historical task is opted in implicitly.
-- name: ListNativeResultTasks :many
SELECT entry.task_data::jsonb AS task_data
FROM session_runs source CROSS JOIN LATERAL
 jsonb_each(COALESCE(source.input_json->'native_task_queue','{}'::jsonb)) AS entry(task_key,task_data)
WHERE source.team_id=public.memoh_current_team_id()
 AND entry.task_data->>'result_delivery_version'='1'
 AND entry.task_data->>'state'<>'queued';

-- name: ListDelegationResultTasks :many
SELECT id,source_bot_id,source_session_id,user_id FROM bot_delegations
WHERE team_id=public.memoh_current_team_id() AND payload->>'result_delivery_version'='1';

-- name: ReadDelegationResultDelivery :one
SELECT COALESCE(result->'result_delivery','{}'::jsonb)::jsonb AS delivery
FROM bot_delegations WHERE team_id=public.memoh_current_team_id() AND id=sqlc.arg(id)::uuid;

-- name: SaveDelegationResultDelivery :one
UPDATE bot_delegations SET result=jsonb_set(result,'{result_delivery}',sqlc.arg(delivery)::jsonb)
WHERE team_id=public.memoh_current_team_id() AND id=sqlc.arg(id)::uuid
 AND COALESCE(result->'result_delivery','{}'::jsonb)=sqlc.arg(expected)::jsonb
RETURNING result->'result_delivery' AS delivery;

-- name: SaveNativeResultDelivery :one
UPDATE session_runs SET input_json=jsonb_set(input_json,
 ARRAY['native_task_queue',sqlc.arg(task_key)::text,'result_delivery'],sqlc.arg(delivery)::jsonb)
WHERE team_id=public.memoh_current_team_id() AND run_id=sqlc.arg(source_run_id)::uuid
 AND bot_id=sqlc.arg(bot_id)::uuid
 AND input_json->'native_task_queue' ? sqlc.arg(task_key)::text
 AND COALESCE(input_json->'native_task_queue'->sqlc.arg(task_key)::text->'result_delivery','{}'::jsonb)=sqlc.arg(expected)::jsonb
RETURNING input_json->'native_task_queue'->sqlc.arg(task_key)::text->'result_delivery' AS delivery;

-- Read the publication and run state in one snapshot: a terminal run cannot
-- later pass the fenced step writer. A missing receipt then permits a summary
-- retry; a committed receipt always wins even if the run subsequently failed.
-- name: TaskResultPublication :one
SELECT r.run_id,r.state,m.id AS message_id
FROM session_runs r
LEFT JOIN bot_history_messages m ON m.team_id=r.team_id AND m.bot_id=r.bot_id
 AND m.session_id=r.session_id AND m.run_id=r.run_id AND m.role='assistant'
 AND m.turn_visible=true AND m.metadata->>'task_result_receipt'=sqlc.arg(receipt)::text
WHERE r.team_id=public.memoh_current_team_id() AND r.bot_id=sqlc.arg(bot_id)::uuid
 AND r.session_id=sqlc.arg(session_id)::uuid AND r.invocation_id=sqlc.arg(invocation_id)::text
ORDER BY m.turn_message_seq DESC NULLS LAST LIMIT 1;
