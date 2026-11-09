-- Retain revoked grants and orphan backlog; a later consumer has its own identity.
ALTER TABLE fleet_service_grant DROP CONSTRAINT IF EXISTS fleet_service_grant_node_id_scope_resource_id_key;
CREATE UNIQUE INDEX IF NOT EXISTS fleet_active_service_grant ON fleet_service_grant(node_id,scope,resource_id) WHERE enabled;
