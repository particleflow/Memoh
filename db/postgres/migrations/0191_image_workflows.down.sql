-- 0191_image_workflows
-- Dropping the two workflow tables removes the configuration and its runs;
-- queued phone album write-backs (0189) are a separate table and stay where
-- they are. The workflow-role columns on album_pairs go back to the bare-target
-- shape: workflow rows are removed first, because the restored single-target
-- index admits one active row per (user, bot).
DROP TABLE IF EXISTS public.image_workflow_runs;
DROP TABLE IF EXISTS public.image_workflows;
DELETE FROM public.album_pairs WHERE workflow_id IS NOT NULL;
ALTER TABLE public.album_pairs DROP CONSTRAINT IF EXISTS album_pairs_role_check;
DROP INDEX IF EXISTS public.album_pairs_active_workflow;
DROP INDEX IF EXISTS public.album_pairs_active_bot;
CREATE UNIQUE INDEX IF NOT EXISTS album_pairs_active_bot
  ON public.album_pairs(team_id, user_id, bot_id) WHERE state = 'active';
ALTER TABLE public.album_pairs DROP COLUMN IF EXISTS role;
ALTER TABLE public.album_pairs DROP COLUMN IF EXISTS workflow_id;
