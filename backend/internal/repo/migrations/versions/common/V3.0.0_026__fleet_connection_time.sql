ALTER TABLE fleet_collaboration_node ADD COLUMN IF NOT EXISTS last_connected_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE fleet_ssh_terminal ADD COLUMN IF NOT EXISTS container_id VARCHAR(64) NOT NULL DEFAULT '';
