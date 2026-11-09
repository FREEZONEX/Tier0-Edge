ALTER TABLE fleet_publication_operation ADD COLUMN IF NOT EXISTS group_id TEXT NOT NULL DEFAULT '';
CREATE TABLE IF NOT EXISTS fleet_stream_group (
 group_id TEXT PRIMARY KEY, stream_id TEXT NOT NULL, release INTEGER NOT NULL,
 enabled BOOLEAN NOT NULL, paused BOOLEAN NOT NULL,
 phase TEXT NOT NULL, last_error TEXT NOT NULL DEFAULT '', created_at BIGINT NOT NULL,
 activated_at BIGINT NOT NULL DEFAULT 0, UNIQUE(stream_id,release)
);
CREATE UNIQUE INDEX IF NOT EXISTS fleet_one_preparing_stream ON fleet_stream_group(stream_id) WHERE phase='preparing';
CREATE TABLE IF NOT EXISTS fleet_stream_preparation (
 group_id TEXT NOT NULL REFERENCES fleet_stream_group(group_id), input_id TEXT NOT NULL,
 node_id TEXT NOT NULL, output_id TEXT NOT NULL, enabled BOOLEAN NOT NULL,
 config JSONB NOT NULL, schema JSONB NOT NULL, contract_hash TEXT NOT NULL, schema_version BIGINT NOT NULL,
 PRIMARY KEY(group_id,input_id)
);
CREATE INDEX IF NOT EXISTS fleet_stream_publication_demand ON fleet_stream_preparation(node_id,output_id) WHERE enabled=true;
