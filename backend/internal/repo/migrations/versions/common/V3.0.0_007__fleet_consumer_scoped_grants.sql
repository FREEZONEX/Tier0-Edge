DROP INDEX IF EXISTS fleet_active_service_grant;
CREATE INDEX IF NOT EXISTS fleet_service_grant_lookup ON fleet_service_grant(node_id,scope,resource_id) WHERE enabled;
