CREATE TABLE IF NOT EXISTS fleet_ssh_terminal (
 id VARCHAR(128) PRIMARY KEY,node_id VARCHAR(128) NOT NULL,actor_id BIGINT NOT NULL,
 api_key_id BIGINT NOT NULL,process_id VARCHAR(128) NOT NULL,state TEXT NOT NULL,
 output BYTEA NOT NULL DEFAULT '',output_offset BIGINT NOT NULL DEFAULT 0,sequence BIGINT NOT NULL DEFAULT 0,
 created_at BIGINT NOT NULL,expires_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS fleet_ssh_terminal_actor ON fleet_ssh_terminal(node_id,actor_id,created_at);
