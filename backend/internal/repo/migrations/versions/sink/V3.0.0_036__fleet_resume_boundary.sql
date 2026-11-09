ALTER TABLE uns_stream_fleet_producer ADD COLUMN IF NOT EXISTS recovery_through BIGINT NOT NULL DEFAULT 0;
