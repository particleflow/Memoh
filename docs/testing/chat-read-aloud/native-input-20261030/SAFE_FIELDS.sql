-- Read only in the unique writer's EXISTING authorized team DB context.
-- Supply the already selected Bot UUID as psql variable bot_id. Do not change
-- RLS/team identity to make this query return rows. No config/key/voice value is
-- selected; only the specific non-secret scalars/boolean presence flags below.
SELECT
  b.id AS bot_id,
  b.tts_model_id,
  m.model_id,
  m.enable AS model_enabled,
  p.client_type,
  p.enable AS provider_enabled,
  COALESCE((COALESCE(p.config, '{}'::jsonb) || COALESCE(m.config, '{}'::jsonb))->>'format', '') AS configured_format,
  COALESCE((COALESCE(p.config, '{}'::jsonb) || COALESCE(m.config, '{}'::jsonb))->>'response_format', '') AS configured_response_format,
  COALESCE((COALESCE(p.config, '{}'::jsonb) || COALESCE(m.config, '{}'::jsonb))->>'sample_rate', '') AS sample_rate,
  COALESCE((COALESCE(p.config, '{}'::jsonb) || COALESCE(m.config, '{}'::jsonb))->>'enable_ssml', 'false') AS enable_ssml,
  COALESCE(length(btrim(COALESCE((COALESCE(p.config, '{}'::jsonb) || COALESCE(m.config, '{}'::jsonb))->>'voice', ''))) > 0, false) AS voice_configured,
  COALESCE(length(btrim(p.config->>'api_key')) > 0, false) AS credential_present,
  COALESCE(length(btrim(p.config->>'base_url')) > 0, false) AS endpoint_override_present,
  CASE WHEN p.config->>'base_url' ~ '^[A-Za-z][A-Za-z0-9+.-]*://'
       THEN lower(split_part(p.config->>'base_url', '://', 1))
       WHEN COALESCE(p.config->>'base_url', '') = '' THEN 'registered-default'
       ELSE 'invalid' END AS endpoint_scheme,
  CASE WHEN p.config->>'base_url' ~ '^[A-Za-z][A-Za-z0-9+.-]*://'
       THEN regexp_replace(split_part(split_part(split_part(split_part(p.config->>'base_url', '://', 2), '/', 1), '?', 1), '#', 1), '^.*@', '')
       ELSE '' END AS endpoint_host,
  CASE WHEN COALESCE(p.config->>'base_url', '') = '' THEN 'registered-default'
       WHEN p.config->>'base_url' ~ '^wss://[^/?#]+/api-ws/v1/inference/?([?#].*)?$' THEN 'dashscope-inference'
       ELSE 'other-redacted' END AS endpoint_path_class,
  COALESCE(p.config->>'base_url' ~ '^[A-Za-z][A-Za-z0-9+.-]*://[^/]*@', false) AS endpoint_has_userinfo,
  COALESCE(position('?' IN p.config->>'base_url') > 0, false) AS endpoint_has_query
FROM bots b
LEFT JOIN models m ON m.id = b.tts_model_id AND m.type = 'speech'
  AND m.team_id = public.memoh_current_team_id()
LEFT JOIN providers p ON p.id = m.provider_id
  AND p.team_id = public.memoh_current_team_id()
WHERE b.id = :'bot_id'::uuid AND b.team_id = public.memoh_current_team_id();
