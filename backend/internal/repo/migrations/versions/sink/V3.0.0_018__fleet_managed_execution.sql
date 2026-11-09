SELECT pg_advisory_xact_lock(814206108);
CREATE TABLE IF NOT EXISTS public.uns_stream_fleet_resource (
 owner_node_id text NOT NULL,
 managed_id text NOT NULL,
 kind text NOT NULL,
 local_id text NOT NULL,
 PRIMARY KEY(owner_node_id,managed_id,kind),
 UNIQUE(kind,local_id)
);
CREATE TABLE IF NOT EXISTS public.uns_stream_fleet_deployment (
 owner_node_id text NOT NULL,
 managed_source_id text NOT NULL,
 generation bigint NOT NULL,
 snapshot_hash text NOT NULL,
 operation_id text NOT NULL,
 source_id text NOT NULL,
 applied_at bigint NOT NULL,
 PRIMARY KEY(owner_node_id,managed_source_id)
);
SELECT public.protect_stream_runtime_storage();
