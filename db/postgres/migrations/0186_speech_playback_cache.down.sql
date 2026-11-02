-- 0186_speech_playback_cache
-- Drop the reuse authority. Any audio files under the data root become
-- unreferenced orphans and are removed by the deployment's normal data-root
-- cleanup; they are not reachable without a row.
DROP TABLE IF EXISTS public.speech_playback_cache;
