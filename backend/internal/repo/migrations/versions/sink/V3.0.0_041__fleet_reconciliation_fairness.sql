ALTER TABLE uns_stream_fleet_source_config_event ADD COLUMN IF NOT EXISTS last_attempt_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE uns_stream_fleet_center_release_event ADD COLUMN IF NOT EXISTS last_attempt_at BIGINT NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS fleet_source_config_retry_order ON uns_stream_fleet_source_config_event(last_attempt_at,created_at,source_id,revision) WHERE phase='queued';
CREATE INDEX IF NOT EXISTS fleet_stream_release_retry_order ON uns_stream_fleet_center_release_event(last_attempt_at,created_at,stream_id,release) WHERE phase='queued';
