SELECT pg_advisory_xact_lock(814206108);
CREATE TABLE IF NOT EXISTS public.uns_stream_fleet_delivery_receipt (
 node_id TEXT NOT NULL, output_id TEXT NOT NULL, event_id TEXT NOT NULL,
 consumer_id TEXT NOT NULL, input_id TEXT NOT NULL, content_hash TEXT NOT NULL,
 written_records BIGINT NOT NULL, committed_at BIGINT NOT NULL,
 PRIMARY KEY(node_id,output_id,event_id,consumer_id,input_id)
);
SELECT public.protect_stream_runtime_storage();
