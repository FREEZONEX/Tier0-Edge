SELECT pg_advisory_xact_lock(814206108);
ALTER TABLE public.uns_stream_fleet_consumer ADD COLUMN IF NOT EXISTS stream_id text NOT NULL DEFAULT '';
ALTER TABLE public.uns_stream_fleet_consumer ADD COLUMN IF NOT EXISTS stream_release integer NOT NULL DEFAULT 0;
ALTER TABLE public.uns_stream_fleet_consumer ADD COLUMN IF NOT EXISTS parameters jsonb NOT NULL DEFAULT '{}';
ALTER TABLE public.uns_stream_fleet_consumer ADD COLUMN IF NOT EXISTS definition jsonb NOT NULL DEFAULT '{}';
ALTER TABLE public.uns_stream_fleet_inbox ADD COLUMN IF NOT EXISTS attempts integer NOT NULL DEFAULT 0;
