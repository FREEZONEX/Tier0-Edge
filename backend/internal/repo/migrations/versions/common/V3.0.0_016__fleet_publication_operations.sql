CREATE TABLE IF NOT EXISTS fleet_consumer_input_state (
 consumer_id TEXT NOT NULL, input_id TEXT NOT NULL, node_id TEXT NOT NULL, output_id TEXT NOT NULL,
 revision BIGINT NOT NULL, active BOOLEAN NOT NULL, paused BOOLEAN NOT NULL,
 max_pending BIGINT NOT NULL DEFAULT 100000, last_error TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(consumer_id,input_id,node_id,output_id)
);
CREATE TABLE IF NOT EXISTS fleet_publication_operation (
 operation_id TEXT PRIMARY KEY, consumer_id TEXT NOT NULL, input_id TEXT NOT NULL,
 consumer_revision BIGINT NOT NULL, node_id TEXT NOT NULL, output_id TEXT NOT NULL,
 policy_revision BIGINT NOT NULL, enabled BOOLEAN NOT NULL, paused BOOLEAN NOT NULL,
 request JSONB NOT NULL, config JSONB NOT NULL, schema JSONB NOT NULL,
 phase TEXT NOT NULL, receipt JSONB NOT NULL, created_at BIGINT NOT NULL,
 UNIQUE(consumer_id,input_id,consumer_revision), UNIQUE(node_id,output_id,policy_revision)
);
CREATE UNIQUE INDEX IF NOT EXISTS fleet_one_pending_publication ON fleet_publication_operation(node_id,output_id) WHERE phase='queued';
