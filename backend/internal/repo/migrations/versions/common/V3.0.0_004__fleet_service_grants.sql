CREATE TABLE IF NOT EXISTS fleet_service_grant (
 grant_id text PRIMARY KEY,
 node_id text NOT NULL,
 scope text NOT NULL,
 resource_id text NOT NULL,
 revision bigint NOT NULL DEFAULT 1,
 enabled boolean NOT NULL DEFAULT true,
 created_by bigint NOT NULL,
 created_at bigint NOT NULL,
 UNIQUE(node_id,scope,resource_id)
);
CREATE TABLE IF NOT EXISTS fleet_proof_nonce (
 node_id text NOT NULL,
 nonce text NOT NULL,
 expires_at bigint NOT NULL,
 PRIMARY KEY(node_id,nonce)
);
CREATE INDEX IF NOT EXISTS fleet_proof_nonce_expiry ON fleet_proof_nonce(expires_at);

ALTER TABLE fleet_collaboration_node ADD COLUMN IF NOT EXISTS installation_id text NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS fleet_collaboration_installation ON fleet_collaboration_node(installation_id) WHERE installation_id <> '';
