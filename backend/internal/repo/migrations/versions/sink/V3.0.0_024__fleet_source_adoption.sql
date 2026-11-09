SELECT pg_advisory_xact_lock(814206108);
CREATE TABLE IF NOT EXISTS public.uns_stream_fleet_adoption (
 operation_id TEXT PRIMARY KEY, owner_node_id TEXT NOT NULL, managed_source_id TEXT NOT NULL,
 local_source_id TEXT NOT NULL UNIQUE, local_revision BIGINT NOT NULL,
 phase TEXT NOT NULL CHECK(phase IN ('prepared','committed')),
 hash TEXT NOT NULL, payload JSONB NOT NULL,
 prepared_at BIGINT NOT NULL, committed_at BIGINT NOT NULL DEFAULT 0,
 UNIQUE(owner_node_id,managed_source_id)
);
SELECT public.protect_stream_runtime_storage();
