-- 0189_phone_album_writebacks
-- Dropping the table discards pending intents the phone has not pulled yet, so
-- roll this back only together with the write-back feature itself.
DROP TABLE IF EXISTS public.phone_album_writebacks;
