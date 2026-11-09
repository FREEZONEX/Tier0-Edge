SELECT pg_advisory_xact_lock(814206108);
ALTER TABLE public.uns_stream_fleet_producer ADD COLUMN IF NOT EXISTS attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE public.uns_stream_fleet_producer ADD COLUMN IF NOT EXISTS next_attempt_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE public.uns_stream_fleet_producer ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '';
SELECT public.protect_stream_runtime_storage();
