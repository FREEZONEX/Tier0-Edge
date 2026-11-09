SELECT pg_advisory_xact_lock(814206108);
CREATE TABLE IF NOT EXISTS public.uns_stream_fleet_subscription (
 output_id text NOT NULL REFERENCES public.uns_stream_fleet_output(output_id),
 consumer_id text NOT NULL,
 active boolean NOT NULL DEFAULT false,
 revision bigint NOT NULL,
 PRIMARY KEY(output_id,consumer_id)
);
INSERT INTO public.uns_stream_fleet_subscription(output_id,consumer_id,active,revision)
 SELECT output_id,consumer_id,active,revision FROM public.uns_stream_fleet_output WHERE consumer_id<>''
 ON CONFLICT DO NOTHING;
DROP INDEX IF EXISTS public.fleet_one_active_consumer;
ALTER TABLE public.uns_stream_fleet_outbox DROP CONSTRAINT IF EXISTS uns_stream_fleet_outbox_pkey;
ALTER TABLE public.uns_stream_fleet_outbox ADD PRIMARY KEY(output_id,event_id,consumer_id);
ALTER TABLE public.uns_stream_fleet_inbox DROP CONSTRAINT IF EXISTS uns_stream_fleet_inbox_pkey;
ALTER TABLE public.uns_stream_fleet_inbox ADD PRIMARY KEY(node_id,output_id,event_id,consumer_id);
SELECT public.protect_stream_runtime_storage();
