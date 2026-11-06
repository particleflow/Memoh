-- 0188_computer_task_batches
-- Batch history is durable like computer_operations. Dropping the table removes
-- pending batches whose items may already hold unsettled operations, so roll
-- this back only together with the batch feature itself.
DROP TABLE IF EXISTS public.computer_task_batches;
