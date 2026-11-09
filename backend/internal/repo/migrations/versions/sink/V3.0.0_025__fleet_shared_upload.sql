SELECT pg_advisory_xact_lock(814206108);
CREATE TABLE IF NOT EXISTS public.uns_stream_fleet_producer (
 output_id TEXT PRIMARY KEY REFERENCES public.uns_stream_fleet_output(output_id),
 owner_node_id TEXT NOT NULL, producer_epoch TEXT NOT NULL,
 sequence BIGINT NOT NULL DEFAULT 0 CHECK(sequence>=0),
 policy_revision BIGINT NOT NULL CHECK(policy_revision>0), publish_enabled BOOLEAN NOT NULL,
 schema_version BIGINT NOT NULL CHECK(schema_version>0), contract_hash TEXT NOT NULL,
 max_events BIGINT NOT NULL CHECK(max_events>0), max_bytes BIGINT NOT NULL CHECK(max_bytes>0),
 updated_at BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS public.uns_stream_fleet_shared_outbox (
 output_id TEXT NOT NULL REFERENCES public.uns_stream_fleet_producer(output_id),
 event_id TEXT NOT NULL, producer_epoch TEXT NOT NULL, sequence BIGINT NOT NULL CHECK(sequence>0),
 policy_revision BIGINT NOT NULL, schema_version BIGINT NOT NULL, contract_hash TEXT NOT NULL,
 collected_at BIGINT NOT NULL, source_id TEXT NOT NULL, binding_id TEXT NOT NULL,
 source_revision BIGINT NOT NULL, stream_id TEXT NOT NULL, stream_release INTEGER NOT NULL,
 generation BIGINT NOT NULL, origin TEXT NOT NULL, trace_id TEXT NOT NULL,
 cause_invocation_id TEXT NOT NULL DEFAULT '', payload JSONB NOT NULL,
 content_hash TEXT NOT NULL, payload_bytes BIGINT NOT NULL, accepted_at BIGINT NOT NULL DEFAULT 0,
 PRIMARY KEY(output_id,event_id), UNIQUE(output_id,producer_epoch,sequence)
);
CREATE INDEX IF NOT EXISTS fleet_shared_outbox_pending ON public.uns_stream_fleet_shared_outbox(output_id,sequence) WHERE accepted_at=0;
CREATE TABLE IF NOT EXISTS public.uns_stream_fleet_upload_batch (
 batch_id TEXT PRIMARY KEY, output_id TEXT NOT NULL REFERENCES public.uns_stream_fleet_producer(output_id),
 hash TEXT NOT NULL, body JSONB NOT NULL, created_at BIGINT NOT NULL, accepted_at BIGINT NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS fleet_one_pending_upload_batch ON public.uns_stream_fleet_upload_batch(output_id) WHERE accepted_at=0;
SELECT public.protect_stream_runtime_storage();
