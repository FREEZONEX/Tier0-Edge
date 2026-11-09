SELECT pg_advisory_xact_lock(814206108);
CREATE TABLE IF NOT EXISTS public.uns_stream_fleet_consumer (
 consumer_id text PRIMARY KEY,
 name text NOT NULL,
 node_id text NOT NULL,
 output_id text NOT NULL,
 collection_id text NOT NULL,
 mapping jsonb NOT NULL,
 active boolean NOT NULL DEFAULT true,
 revision bigint NOT NULL DEFAULT 1,
 configured boolean NOT NULL DEFAULT false,
 created_by bigint NOT NULL,
 created_at bigint NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS fleet_one_active_consumer ON public.uns_stream_fleet_consumer(node_id,output_id) WHERE active;
SELECT public.protect_stream_runtime_storage();
