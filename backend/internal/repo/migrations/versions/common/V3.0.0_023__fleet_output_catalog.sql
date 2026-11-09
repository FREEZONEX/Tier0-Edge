CREATE TABLE IF NOT EXISTS fleet_output_catalog (
 node_id TEXT PRIMARY KEY, items JSONB NOT NULL, refreshed_at BIGINT NOT NULL
);
