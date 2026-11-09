SELECT pg_advisory_xact_lock(814206108);
CREATE UNIQUE INDEX IF NOT EXISTS fleet_one_edge_source_per_node ON public.uns_meta ((data->'config'->'connection'->>'nodeId')) WHERE kind='source' AND deleted_at=0 AND data->>'adapter'='edge' AND jsonb_exists(data->'config','outputBindings');
CREATE TABLE IF NOT EXISTS public.uns_stream_fleet_source_config_event (
 binding_id TEXT NOT NULL, source_id TEXT NOT NULL, revision BIGINT NOT NULL, intent_key TEXT NOT NULL UNIQUE,
 request_hash TEXT NOT NULL, data JSONB NOT NULL, phase TEXT NOT NULL,
 last_error TEXT NOT NULL DEFAULT '', created_by BIGINT NOT NULL, created_at BIGINT NOT NULL,
 PRIMARY KEY(source_id,revision)
);
SELECT public.protect_stream_runtime_storage();
