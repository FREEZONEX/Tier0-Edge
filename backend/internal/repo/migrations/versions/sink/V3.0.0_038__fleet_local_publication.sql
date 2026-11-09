CREATE TABLE IF NOT EXISTS uns_stream_fleet_local_publication (
 output_id TEXT PRIMARY KEY, center_id TEXT NOT NULL, contract_hash TEXT NOT NULL,
 enabled BOOLEAN NOT NULL, revision BIGINT NOT NULL, updated_at BIGINT NOT NULL, updated_by BIGINT NOT NULL
);
SELECT public.protect_stream_runtime_storage();
