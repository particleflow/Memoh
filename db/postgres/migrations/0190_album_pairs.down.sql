-- 0190_album_pairs
-- Dropping the table removes the pairing archive; queued phone album
-- write-backs (0189) are a separate table and stay where they are.
DROP TABLE IF EXISTS public.album_pairs;
