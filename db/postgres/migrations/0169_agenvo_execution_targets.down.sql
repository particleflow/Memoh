-- 0169_agenvo_execution_targets
-- Remove only this provider's registrations and columns. Native registrations remain.
-- Preserve FORCE RLS and use the existing migration pattern of explicit team
-- context. A database owner need not have CREATEROLE or BYPASSRLS privileges.
-- Like 0147, enumerate teams within this migration's single transaction and
-- restore the original policy before commit; failures roll back the change.
ALTER POLICY teams_self_select ON public.teams USING (true);
DO $$
DECLARE
  migration_team_id UUID;
  previous_team_id TEXT := current_setting('memoh.team_id', true);
BEGIN
  FOR migration_team_id IN SELECT id FROM teams LOOP
    PERFORM set_config('memoh.team_id', migration_team_id::text, true);
    DELETE FROM bot_remote_runtime_bindings
      WHERE team_id = migration_team_id
        AND runtime_id IN (SELECT id FROM user_runtimes WHERE provider = 'agenvo');
    DELETE FROM user_runtimes
      WHERE team_id = migration_team_id AND provider = 'agenvo';
  END LOOP;
  PERFORM set_config('memoh.team_id', COALESCE(previous_team_id, ''), true);
END;
$$;
ALTER POLICY teams_self_select ON public.teams
  USING (id = public.memoh_current_team_id());
DROP INDEX IF EXISTS idx_user_runtimes_agenvo_config;
ALTER TABLE user_runtimes DROP COLUMN IF EXISTS provider_config;
ALTER TABLE user_runtimes DROP COLUMN IF EXISTS provider;
