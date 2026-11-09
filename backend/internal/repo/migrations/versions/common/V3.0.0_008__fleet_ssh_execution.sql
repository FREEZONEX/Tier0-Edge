CREATE TABLE IF NOT EXISTS fleet_ssh_profile (
 node_id VARCHAR(128) PRIMARY KEY, host TEXT NOT NULL, port INTEGER NOT NULL,
 username TEXT NOT NULL, auth_type TEXT NOT NULL, credential_ref TEXT NOT NULL,
 host_key_fingerprint TEXT NOT NULL, enabled BOOLEAN NOT NULL DEFAULT false, revision BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS fleet_ssh_execution (
 id VARCHAR(128) PRIMARY KEY, node_id VARCHAR(128) NOT NULL, actor_id BIGINT NOT NULL,
 api_key_id BIGINT NOT NULL, process_id VARCHAR(128) NOT NULL, script TEXT NOT NULL,
 status TEXT NOT NULL, output TEXT NOT NULL DEFAULT '', truncated BOOLEAN NOT NULL DEFAULT false,
 exit_code INTEGER, created_at BIGINT NOT NULL, finished_at BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS fleet_ssh_execution_actor ON fleet_ssh_execution(node_id,actor_id,created_at);
