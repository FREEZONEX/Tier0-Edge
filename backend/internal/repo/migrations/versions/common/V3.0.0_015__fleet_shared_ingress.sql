CREATE TABLE IF NOT EXISTS fleet_ingress_policy (
 node_id TEXT NOT NULL, output_id TEXT NOT NULL, producer_epoch TEXT NOT NULL,
 policy_revision BIGINT NOT NULL, schema_version BIGINT NOT NULL, contract_hash TEXT NOT NULL,
 start_sequence BIGINT NOT NULL CHECK(start_sequence>0), accepted_through BIGINT NOT NULL,
 active BOOLEAN NOT NULL, max_events BIGINT NOT NULL CHECK(max_events>0), max_bytes BIGINT NOT NULL CHECK(max_bytes>0),
 PRIMARY KEY(node_id,output_id)
);
CREATE TABLE IF NOT EXISTS fleet_ingress_policy_version (
 node_id TEXT NOT NULL, output_id TEXT NOT NULL, policy_revision BIGINT NOT NULL,
 producer_epoch TEXT NOT NULL, schema_version BIGINT NOT NULL, contract_hash TEXT NOT NULL,
 source_id TEXT NOT NULL, binding_id TEXT NOT NULL, start_sequence BIGINT NOT NULL,
 end_sequence BIGINT NOT NULL DEFAULT -1, revoked BOOLEAN NOT NULL DEFAULT false, publish_enabled BOOLEAN NOT NULL, schema JSONB NOT NULL,
 PRIMARY KEY(node_id,output_id,policy_revision)
);
CREATE TABLE IF NOT EXISTS fleet_delivery_version (
 consumer_id TEXT NOT NULL, input_id TEXT NOT NULL, revision BIGINT NOT NULL,
 node_id TEXT NOT NULL, output_id TEXT NOT NULL, producer_epoch TEXT NOT NULL,
 start_sequence BIGINT NOT NULL, end_sequence BIGINT NOT NULL DEFAULT -1,
 accepting BOOLEAN NOT NULL, config JSONB NOT NULL,
 PRIMARY KEY(consumer_id,input_id,revision)
);
CREATE INDEX IF NOT EXISTS fleet_delivery_accepting ON fleet_delivery_version(node_id,output_id) WHERE accepting;
CREATE TABLE IF NOT EXISTS fleet_shared_inbox (
 node_id TEXT NOT NULL, output_id TEXT NOT NULL, event_id TEXT NOT NULL,
 producer_epoch TEXT NOT NULL, sequence BIGINT NOT NULL CHECK(sequence>0),
 policy_revision BIGINT NOT NULL, schema_version BIGINT NOT NULL, contract_hash TEXT NOT NULL,
 collected_at BIGINT NOT NULL, source_id TEXT NOT NULL, binding_id TEXT NOT NULL,
 source_revision BIGINT NOT NULL, stream_id TEXT NOT NULL, stream_release INTEGER NOT NULL,
 generation BIGINT NOT NULL, origin TEXT NOT NULL, trace_id TEXT NOT NULL,
 cause_invocation_id TEXT NOT NULL DEFAULT '', payload JSONB NOT NULL,
 content_hash TEXT NOT NULL, payload_bytes BIGINT NOT NULL, accepted_at BIGINT NOT NULL,
 PRIMARY KEY(node_id,output_id,event_id), UNIQUE(node_id,output_id,producer_epoch,sequence)
);
CREATE TABLE IF NOT EXISTS fleet_shared_delivery (
 node_id TEXT NOT NULL, output_id TEXT NOT NULL, event_id TEXT NOT NULL,
 consumer_id TEXT NOT NULL, input_id TEXT NOT NULL, consumer_revision BIGINT NOT NULL,
 producer_epoch TEXT NOT NULL, sequence BIGINT NOT NULL, config JSONB NOT NULL,
 accepted_at BIGINT NOT NULL, committed_at BIGINT NOT NULL DEFAULT 0,
 written_records BIGINT NOT NULL DEFAULT 0, attempts INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(node_id,output_id,event_id,consumer_id,input_id),
 FOREIGN KEY(node_id,output_id,event_id) REFERENCES fleet_shared_inbox(node_id,output_id,event_id)
);
CREATE INDEX IF NOT EXISTS fleet_shared_delivery_pending ON fleet_shared_delivery(consumer_id,input_id,node_id,output_id,sequence) WHERE committed_at=0;
CREATE TABLE IF NOT EXISTS fleet_batch_receipt (
 node_id TEXT NOT NULL, batch_id TEXT NOT NULL, output_id TEXT NOT NULL, hash TEXT NOT NULL,
 event_count INTEGER NOT NULL, accepted_at BIGINT NOT NULL, accepted_through BIGINT NOT NULL,
 PRIMARY KEY(node_id,batch_id)
);
