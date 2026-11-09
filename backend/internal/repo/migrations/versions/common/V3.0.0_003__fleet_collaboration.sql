-- Collaboration registration is separate from legacy MQTT and UNS mirror state.
CREATE TABLE IF NOT EXISTS fleet_collaboration_node (
    node_id VARCHAR(64) PRIMARY KEY REFERENCES fleet_node(fleet_node_id),
    registration_status VARCHAR(24) NOT NULL DEFAULT 'unregistered',
    public_key TEXT NOT NULL DEFAULT '',
    key_id VARCHAR(128) NOT NULL DEFAULT '',
    endpoint TEXT NOT NULL DEFAULT '',
    network_node_id VARCHAR(128),
    registered_at BIGINT NOT NULL DEFAULT 0,
    checked_at BIGINT NOT NULL DEFAULT 0,
    network_status VARCHAR(24) NOT NULL DEFAULT 'unchecked',
    business_status VARCHAR(24) NOT NULL DEFAULT 'unchecked',
    CHECK (registration_status IN ('unregistered', 'registered', 'revoked'))
);
CREATE UNIQUE INDEX IF NOT EXISTS fleet_collaboration_network_identity ON fleet_collaboration_node(network_node_id) WHERE network_node_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS fleet_collaboration_public_key ON fleet_collaboration_node(public_key) WHERE public_key <> '';
CREATE TABLE IF NOT EXISTS fleet_collaboration_enrollment (
    code_hash VARCHAR(64) PRIMARY KEY,
    node_id VARCHAR(64) NOT NULL REFERENCES fleet_node(fleet_node_id),
    expires_at BIGINT NOT NULL,
    consumed_at BIGINT NOT NULL DEFAULT 0,
    proof_hash VARCHAR(64) NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS fleet_collaboration_enrollment_node ON fleet_collaboration_enrollment(node_id);
