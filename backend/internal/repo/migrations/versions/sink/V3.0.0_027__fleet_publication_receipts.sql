SELECT pg_advisory_xact_lock(814206108);
CREATE TABLE IF NOT EXISTS public.uns_stream_fleet_publication_receipt (
 output_id TEXT NOT NULL, policy_revision BIGINT NOT NULL, owner_node_id TEXT NOT NULL,
 producer_epoch TEXT NOT NULL, source_id TEXT NOT NULL, binding_id TEXT NOT NULL,
 start_sequence BIGINT NOT NULL, previous_last_sequence BIGINT NOT NULL,
 publish_enabled BOOLEAN NOT NULL, schema_version BIGINT NOT NULL,
 contract_hash TEXT NOT NULL, applied_at BIGINT NOT NULL,
 PRIMARY KEY(output_id,policy_revision)
);
SELECT public.protect_stream_runtime_storage();
