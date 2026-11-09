-- The global Collection storage queue is the sole delivery path. Retire only
-- the former mirror capture trigger; preserve all records, samples and old queues.
DROP TRIGGER IF EXISTS trg_sync_capture_sample ON public.uns_sample;
UPDATE public.sync_sample_capture SET enabled=false WHERE enabled;
UPDATE public.sync_mount SET enabled=false WHERE enabled;
