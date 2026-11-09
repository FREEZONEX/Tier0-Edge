ALTER TABLE fleet_publication_operation ADD COLUMN IF NOT EXISTS attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE fleet_publication_operation ADD COLUMN IF NOT EXISTS next_attempt_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE fleet_publication_operation ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS fleet_publication_retry ON fleet_publication_operation(next_attempt_at,created_at) WHERE phase='queued';
